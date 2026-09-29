package proc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// helperEnv makes the test binary act as a command (see runHelper).
const helperEnv = "AXX_PROC_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runHelper(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// runHelper implements the commands. Modes:
//
//	exit <code> <out> <err>   write out and err, exit with code
//	cat                       copy the input to the output
//	env <name>                write the variable's value
//	pwd                       write the working directory
//	sleep                     run until stopped
//	grandchild <pidfile>      start a sleeping child that keeps the output
//	                          open, write its pid, exit
//	big <n>                   write n bytes
func runHelper(args []string) int {
	switch args[0] {
	case "exit":
		fmt.Fprint(os.Stdout, args[2])
		fmt.Fprint(os.Stderr, args[3])
		code, _ := strconv.Atoi(args[1])
		return code
	case "cat":
		_, _ = io.Copy(os.Stdout, os.Stdin)
		return 0
	case "env":
		fmt.Print(os.Getenv(args[1]))
		return 0
	case "pwd":
		wd, _ := os.Getwd()
		fmt.Print(wd)
		return 0
	case "sleep":
		time.Sleep(time.Hour)
		return 0
	case "grandchild":
		c := exec.Command(os.Args[0], "sleep")
		c.Env = append(os.Environ(), helperEnv+"=1")
		c.Stdout = os.Stdout
		if err := c.Start(); err != nil {
			return 3
		}
		_ = os.WriteFile(args[1], []byte(strconv.Itoa(c.Process.Pid)), 0o644)
		fmt.Println("started")
		return 0
	case "big":
		n, _ := strconv.Atoi(args[1])
		fmt.Print(strings.Repeat("x", n))
		return 0
	}
	return 2
}

func helper(args ...string) Spec {
	return Spec{Argv: append([]string{os.Args[0]}, args...), Env: append(os.Environ(), helperEnv+"=1")}
}

func TestExitCodeAndStreams(t *testing.T) {
	res, err := Run(context.Background(), helper("exit", "3", "the parcel is dispatched", "cannot cancel"))
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || string(res.Stdout) != "the parcel is dispatched" || string(res.Stderr) != "cannot cancel" || res.TimedOut || res.Truncated {
		t.Errorf("result: %+v", res)
	}
}

func TestInputEnvironmentAndFolder(t *testing.T) {
	s := helper("cat")
	s.Stdin = []byte("PX-ADM-6103\nPX-ADM-6104\n")
	res, err := Run(context.Background(), s)
	if err != nil || string(res.Stdout) != "PX-ADM-6103\nPX-ADM-6104\n" {
		t.Errorf("input: %v %+v", err, res)
	}
	s = helper("env", "PARCELS_SHOP")
	s.Env = append(s.Env, "PARCELS_SHOP=kestrel-books")
	if res, err := Run(context.Background(), s); err != nil || string(res.Stdout) != "kestrel-books" {
		t.Errorf("env: %v %+v", err, res)
	}
	dir := t.TempDir()
	s = helper("pwd")
	s.Dir = dir
	res, err = Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := filepath.EvalSymlinks(string(res.Stdout))
	want, _ := filepath.EvalSymlinks(dir)
	if got != want {
		t.Errorf("dir: %q, want %q", got, want)
	}
	// No input is empty input, never this process's own.
	if res, err := Run(context.Background(), helper("cat")); err != nil || len(res.Stdout) != 0 {
		t.Errorf("no input: %v %+v", err, res)
	}
}

func TestAMissingProgram(t *testing.T) {
	if _, err := Run(context.Background(), Spec{Argv: []string{filepath.Join(t.TempDir(), "parcels-admin")}}); err == nil {
		t.Error("a missing program ran")
	}
	if _, err := Run(context.Background(), Spec{}); err == nil {
		t.Error("no program ran")
	}
}

func TestATimeoutStopsTheCommand(t *testing.T) {
	s := helper("sleep")
	s.Timeout = 200 * time.Millisecond
	start := time.Now()
	res, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Errorf("result: %+v", res)
	}
	if d := time.Since(start); d > stopGrace+5*time.Second {
		t.Errorf("stopping took %s", d)
	}
}

func TestAnEndedContextStopsTheCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	res, err := Run(ctx, helper("sleep"))
	if !errors.Is(err, context.DeadlineExceeded) || res == nil || res.ExitCode != -1 {
		t.Errorf("result: %v %+v", err, res)
	}
}

// A process the command started and left running, holding its output open,
// neither hangs the run nor outlives it, nor holds the folder it ran in.
func TestWhatTheCommandLeftRunningIsStopped(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pid")
	dir, err := os.MkdirTemp("", "axx-proc-")
	if err != nil {
		t.Fatal(err)
	}
	s := helper("grandchild", pidfile)
	s.Dir = dir
	start := time.Now()
	res, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	// At once: no retry, as a pack removes a scenario's folder.
	if err := os.RemoveAll(dir); err != nil {
		t.Errorf("the command's folder: %v", err)
	}
	if res.ExitCode != 0 || !bytes.Contains(res.Stdout, []byte("started")) {
		t.Errorf("result: %+v", res)
	}
	if d := time.Since(start); d > pipesGrace+5*time.Second {
		t.Errorf("the run waited %s", d)
	}
	b, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(string(b))
	deadline := time.Now().Add(5 * time.Second)
	for ProcessAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the grandchild %d still runs", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestOutputIsCappedAtTheLimit(t *testing.T) {
	s := helper("big", "5000")
	s.Limit = 1000
	res, err := Run(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Stdout) != 1000 || !res.Truncated || res.ExitCode != 0 {
		t.Errorf("stdout %d bytes, %+v", len(res.Stdout), res)
	}
}
