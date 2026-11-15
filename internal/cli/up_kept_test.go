package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/filelock"
	"github.com/nimbusxr/axx/internal/lifecycle"
)

// keptFromOutside is a project whose services a supervisor of `axx up`
// keeps that this process can tell nothing of by its id, as a run in a
// sandbox can't of one outside it: the id is of a process that ended, and
// the supervisor's lock is held (by release, which lets go of it).
func keptFromOutside(t *testing.T) (dir string, release func()) {
	t.Helper()
	dir = t.TempDir()
	t.Chdir(dir)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	writeFiles(t, dir, map[string]string{
		"axx.yaml":       "services:\n  api:\n    command: [" + q(exe) + ", never, started]\n",
		"axx-packs.yaml": "packs: [cli]\n",
		"features/kept.feature": "Feature: Kept\n\n  Scenario: a command\n" +
			"    Given the echo command with the following properties:\n      | command | " + q(exe) + " append " + q(filepath.Join(dir, "out")) + " x |\n" +
			"    When the echo command is run\n    Then the echo command's exit code is 0\n",
	})
	gone := exec.Command(exe, "-test.run=^$")
	gone.Env = append(os.Environ(), helperEnv+"=") // the test binary as itself, which ends
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(upRecord{upState: upState{PID: gone.Process.Pid, Services: []string{"api"}, Started: time.Now()}, Locked: true})
	writeFiles(t, dir, map[string]string{".axx/run/up.json": string(b)})
	release, ok, err := filelock.TryLock(lifecycle.UpLock(dir))
	if err != nil || !ok {
		t.Fatalf("the supervisor's lock: %v %v", ok, err)
	}
	return dir, release
}

// A run reuses the services `axx up` keeps, in a project an axx host serves
// too, when it can tell nothing of the supervisor's process: it does not ask
// the host for them, where it would wait for `axx up` to be done with them.
func TestARunReusesKeptServicesItCannotSignal(t *testing.T) {
	t.Setenv(helperEnv, "1")
	dir, release := keptFromOutside(t)
	defer release()
	events := serveHost(t, dir)
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "reusing services started by `axx up`: api") {
		t.Errorf("stderr:\n%s", stderr)
	}
	for _, e := range events() {
		if e.Event == "run" {
			t.Errorf("the run asked the host: %+v", e)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "run", "up.json")); err != nil {
		t.Errorf("the run took the supervisor's record away: %v", err)
	}
	if out, _, _ := run(t, "env", "--json"); !strings.Contains(out, `"up"`) {
		t.Errorf("axx env does not list them: %s", out)
	}
}

// Once the supervisor has gone, its lock is no one's: its record is dropped,
// and a run starts the services.
func TestARunStartsServicesOnceTheSupervisorHasGone(t *testing.T) {
	dir, release := keptFromOutside(t)
	release()
	cfg, err := config.Load(config.LoadOptions{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := liveUp(cfg); ok {
		t.Error("the supervisor has gone: its services are kept")
	}
	if _, err := os.Stat(filepath.Join(dir, ".axx", "run", "up.json")); !os.IsNotExist(err) {
		t.Errorf("its record stays: %v", err)
	}
}

// `axx down` stops a supervisor it cannot signal through the stop file, and
// waits until it let go of its lock.
func TestDownWaitsForASupervisorItCannotSignal(t *testing.T) {
	dir, release := keptFromOutside(t)
	stopped := make(chan time.Time, 1)
	go func() {
		// The supervisor: it stops when the stop file is there.
		for {
			if _, err := os.Stat(filepath.Join(dir, ".axx", "run", "up.stop")); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(300 * time.Millisecond) // its services stopping
		stopped <- time.Now()
		release()
	}()
	stdout, stderr, code := run(t, "down")
	done := time.Now()
	if code != int(exitcode.OK) || !strings.Contains(stdout, "stopped: api") {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	select {
	case at := <-stopped:
		if done.Before(at) {
			t.Error("axx down returned before the supervisor stopped")
		}
	default:
		t.Error("axx down returned before the supervisor stopped")
	}
	for _, f := range []string{"up.json", "up.lock", "up.stop"} {
		if _, err := os.Stat(filepath.Join(dir, ".axx", "run", f)); !os.IsNotExist(err) {
			t.Errorf("%s stays: %v", f, err)
		}
	}
}

// Runs take turns with the services `axx up` keeps: a run waits for the one
// that has them, says so, and goes on once that one is done.
func TestARunWaitsForTheRunThatHasTheKeptServices(t *testing.T) {
	t.Setenv(helperEnv, "1")
	dir, release := keptFromOutside(t)
	defer release()
	waitsForItsTurn(t, dir)
}

// Runs that start the services themselves take turns too.
func TestARunWaitsForTheRunThatStartedTheServices(t *testing.T) {
	dir, _ := hostProject(t)
	t.Setenv(helperEnv, "1") // the service is the test binary
	writeFiles(t, dir, map[string]string{
		"axx-packs.yaml": "packs: [logs, cli]\n",
		"features/host.feature": "Feature: A turn\n\n  Scenario: a command\n" +
			"    Given the echo command with the following properties:\n      | command | " + quoted(t) + " append " + quotedPath(filepath.Join(dir, "out")) + " x |\n" +
			"    When the echo command is run\n    Then the echo command's exit code is 0\n",
	})
	waitsForItsTurn(t, dir)
}

func quotedPath(s string) string { b, _ := json.Marshal(s); return string(b) }

func quoted(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return quotedPath(exe)
}

// waitsForItsTurn runs the project in dir, whose scenario writes the file
// out, while another run has the services: nothing of the scenario runs
// until that run's turn ends.
func waitsForItsTurn(t *testing.T, dir string) {
	t.Helper()
	cfg, err := config.Load(config.LoadOptions{WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	otherEnds, ok, err := filelock.TryLock(turnLock(cfg))
	if err != nil || !ok {
		t.Fatalf("the other run's turn: %v %v", ok, err)
	}
	type result struct {
		stdout, stderr string
		code           int
	}
	done := make(chan result, 1)
	go func() {
		stdout, stderr, code := run(t, "run", "--compact")
		done <- result{stdout, stderr, code}
	}()
	select {
	case r := <-done:
		otherEnds()
		t.Fatalf("the run did not wait: exit %d:\n%s\n%s", r.code, r.stdout, r.stderr)
	case <-time.After(700 * time.Millisecond):
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); !os.IsNotExist(err) {
		t.Errorf("its scenario ran while the other run had the services: %v", err)
	}
	otherEnds()
	select {
	case r := <-done:
		if r.code != int(exitcode.OK) {
			t.Fatalf("exit %d:\n%s\n%s", r.code, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stderr, "waiting for another run of this project to finish with the services") {
			t.Errorf("stderr:\n%s", r.stderr)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the run never took its turn")
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); err != nil {
		t.Errorf("its scenario did not run: %v", err)
	}
	// Its turn ended with it: the next run has the services at once.
	if again, ok, err := filelock.TryLock(turnLock(cfg)); err != nil || !ok {
		t.Errorf("the run kept its turn: %v %v", ok, err)
	} else {
		again()
	}
}
