package lifecycle

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

func TestReadiness(t *testing.T) {
	t.Parallel()
	status := func(code int) *httptest.Server {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }))
		t.Cleanup(srv.Close)
		return srv
	}
	ok, down := status(http.StatusNoContent), status(http.StatusServiceUnavailable)
	fast := func(r config.Ready) *config.Ready {
		r.Interval = config.Duration(20 * time.Millisecond)
		if r.Timeout == 0 {
			r.Timeout = config.Duration(10 * time.Second)
		}
		return &r
	}
	urls := func(u ...string) *config.ReadyHTTP { return &config.ReadyHTTP{URL: u} }

	tests := []struct {
		name  string
		setup func(t *testing.T, app *config.App)
		// wantCode is the expected error code ("" for ready); wantMsg parts
		// must appear in the error.
		wantCode string
		wantMsg  []string
	}{
		{name: "http", setup: func(t *testing.T, app *config.App) {
			addr := freeAddr(t)
			app.Command = helper(t, "http", addr, "100ms")
			app.Ready = fast(config.Ready{HTTP: urls("http://" + addr + "/health")})
		}},
		{name: "all urls 2xx", setup: func(t *testing.T, app *config.App) {
			app.Ready = fast(config.Ready{HTTP: urls(ok.URL, ok.URL+"/other")})
		}},
		{name: "one url not 2xx", setup: func(t *testing.T, app *config.App) {
			app.Ready = fast(config.Ready{HTTP: urls(ok.URL, down.URL), Timeout: config.Duration(300 * time.Millisecond)})
		}, wantCode: CodeNotReady, wantMsg: []string{"not ready after 300ms", "503 Service Unavailable", "apps.api.ready.http.url"}},
		{name: "tcp", setup: func(t *testing.T, app *config.App) {
			addr := freeAddr(t)
			app.Command = helper(t, "tcp", addr, "100ms")
			app.Ready = fast(config.Ready{TCP: addr})
		}},
		{name: "log", setup: func(t *testing.T, app *config.App) {
			app.Command = helper(t, "delayed-print", "100ms", "Started Api in 1.5 seconds")
			app.Ready = fast(config.Ready{Log: `Started \w+ in [\d.]+ seconds`})
		}},
		{name: "exec", setup: func(t *testing.T, app *config.App) {
			marker := filepath.Join(t.TempDir(), "up")
			app.Command = helper(t, "touch", marker, "100ms")
			app.Ready = fast(config.Ready{Exec: helper(t, "exists", marker)})
		}},
		{name: "combined kinds all pass", setup: func(t *testing.T, app *config.App) {
			app.Command = helper(t, "delayed-print", "50ms", "ready now")
			app.Ready = fast(config.Ready{HTTP: urls(ok.URL), Log: "ready now"})
		}},
		{name: "combined kinds one fails", setup: func(t *testing.T, app *config.App) {
			app.Command = helper(t, "print", "ready now")
			app.Ready = fast(config.Ready{HTTP: urls(down.URL), Log: "ready now", Timeout: config.Duration(300 * time.Millisecond)})
		}, wantCode: CodeNotReady, wantMsg: []string{"ready.http.url"}},
		{name: "no readiness config", setup: func(*testing.T, *config.App) {}},
		{name: "timeout", setup: func(t *testing.T, app *config.App) {
			app.Command = helper(t, "print", "still booting")
			app.Ready = fast(config.Ready{Log: "never", Timeout: config.Duration(300 * time.Millisecond)})
		}, wantCode: CodeNotReady, wantMsg: []string{"ready.log: no output line matched never", "still booting", "raise apps.api.ready.timeout"}},
		{name: "early exit fails fast", setup: func(t *testing.T, app *config.App) {
			app.Command = helper(t, "exit", "3", "loading config", "boom: config missing")
			app.Ready = fast(config.Ready{Log: "never", Timeout: config.Duration(time.Minute)})
		}, wantCode: CodeExitedEarly, wantMsg: []string{"app api exited with code 3 before it was ready; last output:", "  boom: config missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := helperApp(t, "api", "sleep")
			tt.setup(t, &app)
			h := newHarness(t, config.Apps{app}, Options{})
			start := time.Now()
			err := h.Start(t.Context(), nil)
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("Start took %s", elapsed)
			}
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Start: %v", err)
				}
				return
			}
			ae := mustCode(t, err, tt.wantCode)
			for _, part := range tt.wantMsg {
				if !strings.Contains(ae.Error()+"\nhint: "+ae.Hint, part) {
					t.Errorf("error does not contain %q:\n%s\nhint: %s", part, ae.Error(), ae.Hint)
				}
			}
			if len(h.Started()) != 0 {
				t.Errorf("Started() = %q after a failed Start", h.Started())
			}
		})
	}
}

func TestNoReadinessWarns(t *testing.T) {
	t.Parallel()
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h.logs.String(), "no readiness check") {
		t.Errorf("no warning logged:\n%s", h.logs)
	}
}
