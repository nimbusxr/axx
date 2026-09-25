package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/exitcode"
)

// recorder is an app that logs "<name> started" and its stop signal to
// file, becomes ready when it prints "started" and logs "<name> cleanup"
// from its cleanup.
func recorder(t *testing.T, file, name string, deps ...string) config.App {
	t.Helper()
	app := logReady(helperApp(t, name, "record", file, name), "^started$")
	app.DependsOn = deps
	app.Cleanup = helper(t, "append", file, name+" cleanup")
	return app
}

// only keeps the lines ending in suffix, without it.
func only(lines []string, suffix string) []string {
	var out []string
	for _, l := range lines {
		if name, ok := strings.CutSuffix(l, " "+suffix); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestStartStopOrder(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		apps func(t *testing.T, file string) config.Apps
		// check verifies start and stop (cleanup) order.
		check func(t *testing.T, started, cleaned []string)
	}{
		{
			name: "declaration order without dependsOn",
			apps: func(t *testing.T, f string) config.Apps {
				return config.Apps{recorder(t, f, "x"), recorder(t, f, "y"), recorder(t, f, "z")}
			},
			check: func(t *testing.T, started, cleaned []string) {
				if want := []string{"x", "y", "z"}; !slices.Equal(started, want) {
					t.Errorf("start order %q, want %q", started, want)
				}
				if want := []string{"z", "y", "x"}; !slices.Equal(cleaned, want) {
					t.Errorf("stop order %q, want %q", cleaned, want)
				}
			},
		},
		{
			name: "dependency graph",
			apps: func(t *testing.T, f string) config.Apps {
				// Declared out of order on purpose.
				return config.Apps{
					recorder(t, f, "web", "api", "worker"),
					recorder(t, f, "api", "db"),
					recorder(t, f, "worker", "db"),
					recorder(t, f, "db"),
				}
			},
			check: func(t *testing.T, started, cleaned []string) {
				if len(started) != 4 || started[0] != "db" || started[3] != "web" {
					t.Errorf("start order %q: want db first and web last", started)
				}
				if len(cleaned) != 4 || cleaned[0] != "web" || cleaned[3] != "db" {
					t.Errorf("stop order %q: want web first and db last", cleaned)
				}
				for _, mid := range []string{"api", "worker"} {
					if p := slices.Index(started, mid); p < 1 || p > 2 {
						t.Errorf("%s started at %d in %q", mid, p, started)
					}
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "events")
			apps := tt.apps(t, file)
			h := newHarness(t, apps, Options{})
			if err := h.Start(t.Context(), nil); err != nil {
				t.Fatal(err)
			}
			if got := h.Started(); len(got) != len(apps) {
				t.Fatalf("Started() = %q", got)
			}
			if err := h.Stop(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := h.Started(); len(got) != 0 {
				t.Errorf("Started() after Stop = %q", got)
			}
			events := readLines(t, file)
			tt.check(t, only(events, "started"), only(events, "cleanup"))
			if runtime.GOOS != "windows" {
				// Every app got SIGTERM before its cleanup ran.
				for _, a := range apps {
					term := slices.Index(events, a.Name+" SIGTERM")
					clean := slices.Index(events, a.Name+" cleanup")
					if term < 0 || term > clean {
						t.Errorf("%s: SIGTERM at %d, cleanup at %d in %q", a.Name, term, clean, events)
					}
				}
			}
		})
	}
}

func TestStartIsIncremental(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "events")
	h := newHarness(t, config.Apps{recorder(t, file, "db"), recorder(t, file, "api"), recorder(t, file, "web")}, Options{})
	if err := h.Start(t.Context(), []string{"api"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(t.Context(), []string{"api", "db"}); err != nil {
		t.Fatal(err)
	}
	if got, want := h.Started(), []string{"api", "db"}; !slices.Equal(got, want) {
		t.Errorf("Started() = %q, want %q", got, want)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Without dependsOn, stop order is the reverse of start order.
	if got, want := only(readLines(t, file), "cleanup"), []string{"db", "api"}; !slices.Equal(got, want) {
		t.Errorf("stop order %q, want %q", got, want)
	}
}

func TestStartUnknownApp(t *testing.T) {
	t.Parallel()
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{})
	wantCode(t, h.Start(t.Context(), []string{"apii"}), CodeUnknownApp)
}

func TestDisabledAppsAreSkipped(t *testing.T) {
	t.Parallel()
	off := disabled(helperApp(t, "legacy", "exit", "1"))
	h := newHarness(t, config.Apps{off, helperApp(t, "api", "sleep")}, Options{})
	if err := h.Start(t.Context(), []string{"legacy", "api"}); err != nil {
		t.Fatal(err)
	}
	if got := h.Started(); !slices.Equal(got, []string{"api"}) {
		t.Errorf("Started() = %q", got)
	}
}

func TestOutputIsPrefixedAndTailed(t *testing.T) {
	t.Parallel()
	app := logReady(helperApp(t, "api", "print", "hello", "err:oops", "done"), "done")
	h := newHarness(t, config.Apps{app}, Options{})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	eventually(t, 2*time.Second, "stderr line", func() bool { return h.stderr.String() != "" })
	if got := h.stdout.String(); got != "[api] hello\n[api] done\n" {
		t.Errorf("stdout = %q", got)
	}
	if got := h.stderr.String(); got != "[api] oops\n" {
		t.Errorf("stderr = %q", got)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	tail := h.Tail("api")
	for _, want := range []string{"hello", "oops", "done"} {
		if !slices.Contains(tail, want) {
			t.Errorf("Tail() = %q, missing %q", tail, want)
		}
	}
	if h.Tail("nope") != nil {
		t.Error("Tail of unknown app is not nil")
	}
}

func TestWorkingDirectory(t *testing.T) {
	t.Parallel()
	app := logReady(helperApp(t, "api", "pwd"), "^pwd=")
	app.Dir = "svc"
	h := newHarness(t, config.Apps{app}, Options{})
	if err := os.Mkdir(filepath.Join(h.dir, "svc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(filepath.Join(h.dir, "svc"))
	got := strings.TrimSpace(strings.TrimPrefix(h.stdout.String(), "[api] pwd="))
	if got, _ = filepath.EvalSymlinks(got); got != want {
		t.Errorf("app ran in %q, want %q", got, want)
	}
}

func TestStartErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		app      func(t *testing.T) config.App
		wantCode string
		wantHint string
	}{
		{"missing dir", func(t *testing.T) config.App {
			a := helperApp(t, "api", "sleep")
			a.Dir = "nope"
			return a
		}, CodeBadDir, "apps.api.dir"},
		{"missing executable", func(*testing.T) config.App {
			return config.App{Name: "api", Command: config.Command{Line: "axx-no-such-binary --port 1"}}
		}, CodeLaunchFailed, "apps.api.command"},
		{"no command", func(*testing.T) config.App {
			return config.App{Name: "api"}
		}, CodeNoCommand, "apps.api.command"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			h := newHarness(t, config.Apps{tt.app(t)}, Options{})
			ae := mustCode(t, h.Start(t.Context(), nil), tt.wantCode)
			if !strings.Contains(ae.Hint, tt.wantHint) {
				t.Errorf("hint %q does not mention %s", ae.Hint, tt.wantHint)
			}
		})
	}
}

func TestCleanupAlwaysRuns(t *testing.T) {
	t.Parallel()
	t.Run("app crashes before ready", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "events")
		db := recorder(t, file, "db")
		api := logReady(helperApp(t, "api", "exit", "2", "fatal: no config"), "never")
		api.Cleanup = helper(t, "append", file, "api cleanup")
		h := newHarness(t, config.Apps{db, api}, Options{})

		wantCode(t, h.Start(t.Context(), nil), CodeExitedEarly)
		// Start tore everything down itself: api cleaned first, then db.
		if got, want := only(readLines(t, file), "cleanup"), []string{"api", "db"}; !slices.Equal(got, want) {
			t.Errorf("cleanups %q, want %q", got, want)
		}
		if got := h.Started(); len(got) != 0 {
			t.Errorf("Started() = %q", got)
		}
		if tail := h.Tail("api"); !slices.Contains(tail, "fatal: no config") {
			t.Errorf("Tail(api) = %q", tail)
		}
	})
	t.Run("app crashes after ready", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "events")
		api := logReady(helperApp(t, "api", "print-exit", "50ms", "1", "up"), "up")
		api.Cleanup = helper(t, "append", file, "api cleanup")
		h := newHarness(t, config.Apps{api}, Options{})
		if err := h.Start(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		eventually(t, 5*time.Second, "crash", func() bool { return strings.Contains(h.logs.String(), "exited on its own") })
		if err := h.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := readLines(t, file); !slices.Equal(got, []string{"api cleanup"}) {
			t.Errorf("events %q", got)
		}
	})
	t.Run("cleanup failure is reported and others still run", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "events")
		db := recorder(t, file, "db")
		api := helperApp(t, "api", "sleep")
		api.Cleanup = helper(t, "exit", "4", "cannot drop schema")
		h := newHarness(t, config.Apps{db, api}, Options{})
		if err := h.Start(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		ae := mustCode(t, h.Stop(t.Context()), CodeCleanupFailed)
		if !strings.Contains(ae.Error(), "exited with code 4") || !strings.Contains(ae.Error(), "cannot drop schema") {
			t.Errorf("error = %v", ae)
		}
		if got := only(readLines(t, file), "cleanup"); !slices.Equal(got, []string{"db"}) {
			t.Errorf("cleanups %q", got)
		}
	})
}

func TestCancelDuringStart(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "events")
	a := recorder(t, file, "a")
	b := logReady(helperApp(t, "b", "sleep"), "never")
	b.Cleanup = helper(t, "append", file, "b cleanup")
	h := newHarness(t, config.Apps{a, b}, Options{})

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		// b is launched once a is ready; then press "Ctrl-C".
		for !slices.Contains(h.Started(), "b") {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	err := h.Start(ctx, nil)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Start took %s to return after cancel", elapsed)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	ae := mustCode(t, err, CodeInterrupted)
	if ae.Exit != exitcode.Interrupted || axxerr.ExitCode(err) != exitcode.Interrupted {
		t.Errorf("exit code = %v", ae.Exit)
	}
	if got, want := only(readLines(t, file), "cleanup"), []string{"b", "a"}; !slices.Equal(got, want) {
		t.Errorf("cleanups %q, want %q", got, want)
	}
	if got := h.Started(); len(got) != 0 {
		t.Errorf("Started() = %q", got)
	}
}

func TestAttach(t *testing.T) {
	t.Parallel()
	var up atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !up.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)
	file := filepath.Join(t.TempDir(), "events")
	attached := func(timeout time.Duration) config.App {
		return config.App{
			Name:    "api",
			Command: config.Command{Line: "axx-no-such-binary"}, // never run
			Cleanup: helper(t, "append", file, "api cleanup"),
			Env:     helperEnvVars(),
			Ready: &config.Ready{
				HTTP:     &config.ReadyHTTP{URL: config.StringList{srv.URL}},
				Timeout:  config.Duration(timeout),
				Interval: config.Duration(20 * time.Millisecond),
			},
		}
	}

	t.Run("waits for readiness without starting", func(t *testing.T) {
		h := newHarness(t, config.Apps{attached(10 * time.Second)}, Options{Attach: map[string]bool{"api": true}})
		time.AfterFunc(200*time.Millisecond, func() { up.Store(true) })
		start := time.Now()
		if err := h.Start(t.Context(), nil); err != nil {
			t.Fatal(err)
		}
		if elapsed := time.Since(start); elapsed < 150*time.Millisecond {
			t.Errorf("Start returned after %s, before the app was ready", elapsed)
		}
		if got := h.Started(); len(got) != 0 {
			t.Errorf("Started() = %q", got)
		}
		if err := h.Stop(t.Context()); err != nil {
			t.Fatal(err)
		}
		if got := readLines(t, file); got != nil {
			t.Errorf("cleanup ran for an attached app: %q", got)
		}
	})
	t.Run("not ready explains attach", func(t *testing.T) {
		up.Store(false)
		h := newHarness(t, config.Apps{attached(200 * time.Millisecond)}, Options{Attach: map[string]bool{"api": true}})
		ae := mustCode(t, h.Start(t.Context(), nil), CodeNotReady)
		if !strings.Contains(ae.Hint, "attached") {
			t.Errorf("hint = %q", ae.Hint)
		}
	})
}

func TestNoStart(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "events")
	app := logReady(helperApp(t, "api", "append", file, "started"), "never")
	app.Cleanup = helper(t, "append", file, "cleanup")
	h := newHarness(t, config.Apps{app}, Options{NoStart: true, Attach: map[string]bool{"api": true}})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := readLines(t, file); got != nil {
		t.Errorf("NoStart ran something: %q", got)
	}
}
