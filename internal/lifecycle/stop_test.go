package lifecycle

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

func TestStopSignals(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, "Windows apps are terminated without a signal")
	const grace = 300 * time.Millisecond
	tests := []struct {
		name   string
		mode   string
		signal string
		// wantEvents are the lines the app records; wantSlow means the
		// grace period had to run out.
		wantEvents []string
		wantSlow   bool
	}{
		{"SIGTERM by default", "record", "", []string{"api started", "api SIGTERM"}, false},
		{"SIGINT when configured", "record", "SIGINT", []string{"api started", "api SIGINT"}, false},
		{"SIGKILL after grace", "stubborn", "", []string{"SIGTERM"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "events")
			app := logReady(helperApp(t, "api", tt.mode, file, "api"), "^(started|ready)$")
			app.Stop = config.Stop{Signal: tt.signal, Grace: config.Duration(grace)}
			h := newHarness(t, config.Apps{app}, Options{})
			if err := h.Start(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			pid := h.pidOf(t, "api")
			start := time.Now()
			if err := h.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(start)
			if tt.wantSlow && elapsed < grace {
				t.Errorf("stopped after %s, before the %s grace period", elapsed, grace)
			}
			if !tt.wantSlow && elapsed >= grace {
				t.Errorf("graceful stop took %s", elapsed)
			}
			if processAlive(pid) {
				t.Errorf("process %d is still alive", pid)
			}
			if got := readLines(t, file); !slices.Equal(got, tt.wantEvents) {
				t.Errorf("events %q, want %q", got, tt.wantEvents)
			}
		})
	}
}

func TestStopKillsProcessGroup(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, "process groups")
	dir := t.TempDir()
	pidFile, events := filepath.Join(dir, "pid"), filepath.Join(dir, "events")
	// The app exits on SIGTERM; its child ignores SIGTERM and must be
	// SIGKILLed with the group once the grace period is over.
	app := logReady(helperApp(t, "api", "grandchild", pidFile, events), "spawned")
	app.Stop.Grace = config.Duration(200 * time.Millisecond)
	h := newHarness(t, config.Apps{app}, Options{})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	child, _ := strconv.Atoi(strings.TrimSpace(string(data)))
	if !processAlive(child) {
		t.Fatalf("grandchild %d is not running", child)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, "grandchild to die", func() bool { return !processAlive(child) })
	if got := readLines(t, events); !slices.Equal(got, []string{"SIGTERM"}) {
		t.Errorf("grandchild events %q, want it to have seen SIGTERM", got)
	}
}

func TestStopWithCancelledContextKillsAtOnce(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, "stop signals")
	file := filepath.Join(t.TempDir(), "events")
	app := logReady(helperApp(t, "api", "stubborn", file), "ready")
	app.Stop.Grace = config.Duration(time.Minute)
	h := newHarness(t, config.Apps{app}, Options{})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	if err := h.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Stop with a cancelled context took %s", elapsed)
	}
}

func TestCheckSignal(t *testing.T) {
	for _, name := range []string{"", "SIGTERM", "term", " sigint ", "INT", "SIGKILL"} {
		if err := checkSignal(name); err != nil {
			t.Errorf("checkSignal(%q): %v", name, err)
		}
	}
	for _, name := range []string{"SIGWINCH", "15", "TERMINATE"} {
		if err := checkSignal(name); err == nil {
			t.Errorf("checkSignal(%q) accepted", name)
		}
	}
}
