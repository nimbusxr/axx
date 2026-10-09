//go:build darwin

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary plays the programs: AXX_LAUNCHER_TEST names the part.
const (
	partEnv = "AXX_LAUNCHER_TEST"
	outEnv  = "AXX_LAUNCHER_OUT"
)

func TestMain(m *testing.M) {
	switch os.Getenv(partEnv) {
	case "child":
		// Reports itself, then starts a child of its own (an app axx starts)
		// that reports itself too.
		report("child")
		cmd := exec.Command(os.Args[0], "-test.run=^$")
		cmd.Env = append(os.Environ(), partEnv+"=grandchild")
		if err := cmd.Run(); err != nil {
			report("grandchild-error " + err.Error())
		}
		os.Exit(0)
	case "grandchild":
		report("grandchild")
		os.Exit(0)
	case "launcher":
		// The launcher, running what follows "--".
		args := os.Args[len(os.Args)-2:]
		code, err := Run(args[0], args)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(127)
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// report writes the part's process, and the process macOS holds responsible
// for it, to the test's file.
func report(part string) {
	r, err := ResponsibleFor(os.Getpid())
	line := fmt.Sprintf("%s %d %d %v\n", part, os.Getpid(), r, err)
	f, ferr := os.OpenFile(os.Getenv(outEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// reports are the parts' reports: each part's process and the one
// responsible for it.
func reports(t *testing.T, path string) map[string][2]int {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][2]int{}
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			t.Fatalf("a report: %q", l)
		}
		pid, _ := strconv.Atoi(f[1])
		r, _ := strconv.Atoi(f[2])
		out[f[0]] = [2]int{pid, r}
	}
	return out
}

// A program Start starts is responsible for itself, and for the programs it
// starts: what they ask permission for is asked of it.
func TestStartMakesTheProgramResponsible(t *testing.T) {
	out := filepath.Join(t.TempDir(), "reports")
	env := append(os.Environ(), partEnv+"=child", outEnv+"="+out)
	pid, err := Start(os.Args[0], []string{os.Args[0], "-test.run=^$"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := Wait(pid); err != nil || code != 0 {
		t.Fatalf("the child: %d %v", code, err)
	}
	r := reports(t, out)
	child, grandchild := r["child"], r["grandchild"]
	if child[0] != pid || child[1] != pid {
		t.Errorf("the child %d is held responsible to %d, want itself", child[0], child[1])
	}
	if grandchild[0] == 0 || grandchild[1] != pid {
		t.Errorf("the child's child %d is held responsible to %d, want the child %d (reports %v)", grandchild[0], grandchild[1], pid, r)
	}
}

// Started as programs are (exec), a program is not responsible for the
// programs it starts: under a terminal or IDE the app is; with no app above
// it (a background job), each is responsible for itself alone.
func TestWithoutStartAProgramIsNotResponsibleForItsChildren(t *testing.T) {
	out := filepath.Join(t.TempDir(), "reports")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), partEnv+"=child", outEnv+"="+out)
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	r := reports(t, out)
	child, grandchild := r["child"], r["grandchild"]
	if grandchild[0] == 0 || grandchild[1] == child[0] {
		t.Errorf("the child's child %d is held responsible to the child %d: want another (reports %v)", grandchild[0], child[0], r)
	}
}

// Run passes its program's exit status back, and a signal's as 128 plus its
// number.
func TestRunPassesTheExitStatusBack(t *testing.T) {
	for script, want := range map[string]int{"exit 0": 0, "exit 3": 3, "kill -TERM $$": 128 + int(syscall.SIGTERM)} {
		code, err := Run("/bin/sh", []string{"sh", "-c", script})
		if err != nil || code != want {
			t.Errorf("%q: %d %v, want %d", script, code, err, want)
		}
	}
}

// An interrupt (Ctrl-C) to the launcher passes on to its program, which
// stops, and the launcher with it.
func TestRunPassesAnInterruptOn(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^$", "--", "/bin/sleep", "30")
	cmd.Env = append(os.Environ(), partEnv+"=launcher")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("the launcher did not stop on an interrupt")
	}
	if got, want := cmd.ProcessState.ExitCode(), 128+int(syscall.SIGINT); got != want {
		t.Errorf("exit status %d, want %d (its program stopped by the interrupt)", got, want)
	}
}
