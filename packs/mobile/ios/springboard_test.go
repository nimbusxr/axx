package mobileios

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// What the library reported counts only while the SpringBoard it reported from runs: a
// simulator booted again (or shut down) has none of it.
func TestTheSpringBoardLibraryReportsForTheSpringBoardThatRuns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iOS simulators run on macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(springboardStatuses(), 0o755); err != nil {
		t.Fatal(err)
	}
	report := func(udid string, pid int, status string) {
		t.Helper()
		line := strconv.Itoa(pid) + " " + status + "\n"
		if err := os.WriteFile(filepath.Join(springboardStatuses(), udid), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	report("RUNNING", os.Getpid(), "ok")
	report("FAILED", os.Getpid(), "this iOS's Cover Sheet has no -[CSCombinedListViewController _hasProximitySensor] to give a proximity sensor")
	report("REBOOTED", gone.Process.Pid, "ok")
	for udid, want := range map[string]string{
		"RUNNING":  "ok",
		"FAILED":   "this iOS's Cover Sheet has no -[CSCombinedListViewController _hasProximitySensor] to give a proximity sensor",
		"REBOOTED": "",
		"NEVER":    "",
	} {
		if got := springboardStatus(udid); got != want {
			t.Errorf("%s: %q, want %q", udid, got, want)
		}
	}
	if env := strings.Join(springboardEnv("/lib.dylib"), " "); !strings.Contains(env, "SIMCTL_CHILD_DYLD_INSERT_LIBRARIES=/lib.dylib") || !strings.Contains(env, "SIMCTL_CHILD_AXX_SPRINGBOARD_STATUS="+springboardStatuses()) {
		t.Errorf("simctl is given %s", env)
	}
}

// The library builds with Xcode for the Mac's simulators, once: a second suite finds it built.
func TestTheSpringBoardLibraryBuildsForTheSimulators(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("iOS simulators run on macOS only")
	}
	t.Setenv("HOME", t.TempDir())
	lib, err := springboardLibrary(context.Background(), core.NewSuite(core.SuiteOptions{}))
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("file", lib).Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "dynamically linked shared library") || !strings.Contains(string(out), simulatorArch(context.Background())) {
		t.Errorf("built %s", out)
	}
	if _, err := os.Stat(springboardStatuses()); err != nil {
		t.Errorf("no folder for its reports: %v", err)
	}
	again, err := springboardLibrary(context.Background(), core.NewSuite(core.SuiteOptions{}))
	if err != nil || again != lib {
		t.Errorf("built again: %q, %v", again, err)
	}
}
