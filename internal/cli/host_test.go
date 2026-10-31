package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/host"
)

// hostProject is a project of one service, the test binary, whose output
// a feature checks in the console log the run keeps; its cleanup appends to
// cleanups.
func hostProject(t *testing.T) (dir, cleanups string) {
	t.Helper()
	dir = t.TempDir()
	t.Chdir(dir)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cleanups = filepath.Join(dir, "cleanups")
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	writeFiles(t, dir, map[string]string{
		"axx.yaml": "services:\n  api:\n" +
			"    command: [" + q(exe) + ", parcels, api, ready]\n" +
			"    cleanup: [" + q(exe) + ", append, " + q(cleanups) + ", api]\n" +
			"    ready:\n      log: parcels api ready\n      timeout: 20s\n",
		"axx-packs.yaml": "packs: [logs]\n",
		"features/host.feature": `Feature: Services from the axx host
  Scenario: the service's output is in the run's console log
    Given the console log with the following properties:
      | url | file://.axx/logs/services.log |
    Then within 5s the console log has an entry matching "parcels api serving"
`,
	})
	return dir, cleanups
}

// serveHost serves the project in dir until the test ends, and keeps its
// events.
func serveHost(t *testing.T, dir string) func() []host.Event {
	t.Helper()
	var mu sync.Mutex
	var events []host.Event
	srv, err := host.New(context.Background(), host.Options{
		Load:      config.LoadOptions{WorkDir: dir},
		Approver:  host.NewTerminal(strings.NewReader(""), &strings.Builder{}, false, false),
		Events:    func(e host.Event) { mu.Lock(); events = append(events, e); mu.Unlock() },
		Approvals: "-",
		Env:       append(os.Environ(), helperEnv+"=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.Serve(ctx)
	}()
	t.Cleanup(func() { cancel(); <-done })
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if a, _ := host.Find(dir); a != nil {
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("the host did not announce itself")
		}
	}
	return func() []host.Event {
		mu.Lock()
		defer mu.Unlock()
		return append([]host.Event(nil), events...)
	}
}

// A run in a project an axx host serves has its services from the host: they
// start there, their output comes back to the run's console log, and the host
// cleans up after the run.
func TestRunHasItsServicesFromTheHost(t *testing.T) {
	dir, cleanups := hostProject(t)
	events := serveHost(t, dir)
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "axx: the axx host started api") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if b, _ := os.ReadFile(cleanups); strings.TrimSpace(string(b)) != "api" {
		t.Errorf("cleanups: %q", b)
	}
	var ended *host.Event
	for _, e := range events() {
		if e.Event == "run" && e.State == host.StateEnded {
			ended = &e
		}
	}
	if ended == nil || ended.Result == nil || ended.Result.Passed != 1 || ended.Error != nil || !strings.HasPrefix(ended.Command, "axx ") {
		t.Errorf("the host's run: %+v", ended)
	}
}

// A host that left its announcement behind, and no longer answers, leaves the
// run to start its services itself.
func TestRunStartsServicesWhenTheHostIsGone(t *testing.T) {
	dir, cleanups := hostProject(t)
	t.Setenv(helperEnv, "1") // the services this run starts are the test binary too
	b, _ := json.Marshal(host.Announcement{Addr: "127.0.0.1:1", Token: "gone", PID: 1})
	writeFiles(t, dir, map[string]string{".axx/host.json": string(b)})
	stdout, stderr, code := run(t, "run", "--compact")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "does not answer") || !strings.Contains(stderr, "starting the services here") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if b, _ := os.ReadFile(cleanups); strings.TrimSpace(string(b)) != "api" {
		t.Errorf("cleanups: %q", b)
	}
}
