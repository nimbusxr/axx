package lifecycle

import (
	"net"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

func TestIDELineFormat(t *testing.T) {
	d := debugger{typ: "java", host: "localhost", port: 5005, mode: modeIDEListens}
	if got, want := d.requestLine("orders-api"), "[AXX-IDE] debug-listener-request name=orders-api type=java host=localhost port=5005"; got != want {
		t.Errorf("requestLine = %q, want %q", got, want)
	}
	d = debugger{typ: "go", host: "127.0.0.1", port: 2345, mode: modeAppListens}
	if got, want := d.attachLine("pricing"), "[AXX-IDE] debug-attach-request name=pricing type=go host=127.0.0.1 port=2345"; got != want {
		t.Errorf("attachLine = %q, want %q", got, want)
	}
}

func TestParseDebugDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   config.Debugger
		want debugger
	}{
		{"java listens in the IDE", config.Debugger{Port: 5005}, debugger{typ: "java", host: "localhost", port: 5005, mode: modeIDEListens}},
		{"go app listens", config.Debugger{Type: "go", Port: 2345}, debugger{typ: "go", host: "localhost", port: 2345, mode: modeAppListens}},
		{"explicit", config.Debugger{Type: "python", Host: "10.0.0.2", Port: 5678, Mode: modeIDEListens}, debugger{typ: "python", host: "10.0.0.2", port: 5678, mode: modeIDEListens}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := tt.in
			spec, err := parseDebug(config.App{Name: "api", Debug: &config.Debug{Debugger: &d}})
			if err != nil {
				t.Fatal(err)
			}
			if *spec.debugger != tt.want {
				t.Errorf("debugger = %+v, want %+v", *spec.debugger, tt.want)
			}
			if spec.onUnavailable != onUnavailableRetry || spec.attempts != defaultDebugAttempts || spec.delay != defaultDebugDelay {
				t.Errorf("retry defaults = %s %d %s", spec.onUnavailable, spec.attempts, spec.delay)
			}
		})
	}
}

// port returns the port of a "host:port" address.
func port(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(p)
	return n
}

func TestDebugMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		debugger *config.Debugger // Port is filled in
		debug    config.Debug
		opts     Options
		// listening: a debugger listens from the start; lateListen: only
		// after 150ms.
		listening, lateListen bool
		wantCommand           string // "normal" or "debug"; "" = not started
		wantCode              string
		wantRequests          int  // listener-request lines
		wantAttach            bool // attach-request line
	}{
		{
			name: "debug not requested", debugger: &config.Debugger{}, listening: true,
			wantCommand: "normal",
		},
		{
			name: "listening debugger", debugger: &config.Debugger{}, listening: true, opts: Options{Debug: map[string]bool{"api": true}},
			wantCommand: "debug", wantRequests: 1,
		},
		{
			name: "debug all", debugger: &config.Debugger{}, listening: true, opts: Options{DebugAll: true},
			wantCommand: "debug", wantRequests: 1,
		},
		{
			name: "debug command without debugger", opts: Options{DebugAll: true},
			wantCommand: "debug",
		},
		{
			name: "fail when not listening", debugger: &config.Debugger{}, debug: config.Debug{OnUnavailable: "fail"}, opts: Options{DebugAll: true},
			wantCode: CodeDebuggerUnavailable, wantRequests: 1,
		},
		{
			name: "fallback when not listening", debugger: &config.Debugger{}, debug: config.Debug{OnUnavailable: "fallback"}, opts: Options{DebugAll: true},
			wantCommand: "normal", wantRequests: 1,
		},
		{
			name: "retry gives up", debugger: &config.Debugger{},
			debug: config.Debug{Retry: config.Retry{Attempts: 2, Delay: config.Duration(20 * time.Millisecond)}}, opts: Options{DebugAll: true},
			wantCode: CodeDebuggerUnavailable, wantRequests: 3,
		},
		{
			name: "retry succeeds once the IDE listens", debugger: &config.Debugger{}, lateListen: true,
			debug: config.Debug{Retry: config.Retry{Attempts: 50, Delay: config.Duration(50 * time.Millisecond)}}, opts: Options{DebugAll: true},
			wantCommand: "debug", wantRequests: -1,
		},
		{
			name: "app listens", debugger: &config.Debugger{Type: "go"}, listening: true, opts: Options{DebugAll: true},
			wantCommand: "debug", wantAttach: true,
		},
		{
			name: "app listens but debug port never opens", debugger: &config.Debugger{Type: "go"}, opts: Options{DebugAll: true},
			wantCommand: "debug", wantAttach: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(t.TempDir(), "events")
			addr := freeAddr(t)
			if tt.listening {
				addr = listenAddr(t)
			}
			app := logReady(helperApp(t, "api", "record", file, "normal"), "^started$")
			app.Debug = &tt.debug
			app.Debug.Command = helper(t, "record", file, "debug")
			if tt.debugger != nil {
				d := *tt.debugger
				d.Port = port(t, addr)
				app.Debug.Debugger = &d
			}
			if tt.lateListen {
				var mu sync.Mutex
				var late net.Listener
				timer := time.AfterFunc(150*time.Millisecond, func() {
					ln, err := net.Listen("tcp", addr)
					if err == nil {
						mu.Lock()
						late = ln
						mu.Unlock()
					}
				})
				t.Cleanup(func() {
					timer.Stop()
					mu.Lock()
					defer mu.Unlock()
					if late != nil {
						_ = late.Close()
					}
				})
			}
			h := newHarness(t, config.Apps{app}, tt.opts)
			err := h.Start(t.Context(), nil)

			if tt.wantCode != "" {
				ae := mustCode(t, err, tt.wantCode)
				if msg := ae.Error(); !strings.Contains(msg, "localhost:"+strconv.Itoa(port(t, addr))) || !strings.Contains(msg, "To fix this:") {
					t.Errorf("message does not explain the fix:\n%s", msg)
				}
				if !strings.Contains(ae.Hint, "onUnavailable") {
					t.Errorf("hint = %q", ae.Hint)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			var started []string
			if tt.wantCommand != "" {
				started = []string{tt.wantCommand + " started"}
			}
			if got := readLines(t, file); !slices.Equal(got, started) {
				t.Errorf("events %q, want %q", got, started)
			}

			request := "[AXX-IDE] debug-listener-request name=api type=java host=localhost port=" + strconv.Itoa(port(t, addr))
			attach := "[AXX-IDE] debug-attach-request name=api type=go host=localhost port=" + strconv.Itoa(port(t, addr))
			lines := strings.Split(h.stdout.String(), "\n")
			requests := 0
			for _, l := range lines {
				if l == request {
					requests++
				} else if strings.Contains(l, "debug-listener-request") {
					t.Errorf("malformed request line %q", l)
				}
			}
			switch {
			case tt.wantRequests < 0 && requests < 2:
				t.Errorf("got %d request lines, want it re-printed on retry", requests)
			case tt.wantRequests >= 0 && requests != tt.wantRequests:
				t.Errorf("got %d request lines, want %d:\n%s", requests, tt.wantRequests, h.stdout)
			}
			if got := slices.Contains(lines, attach); got != tt.wantAttach {
				t.Errorf("attach line present = %v, want %v:\n%s", got, tt.wantAttach, h.stdout)
			}
		})
	}
}
