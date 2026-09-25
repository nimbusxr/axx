package lifecycle

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/config"
)

// A command like `docker compose up -d` starts the app and exits 0: axx must
// keep waiting for readiness instead of failing.
func TestLauncherExitZeroKeepsWaiting(t *testing.T) {
	var ready atomic.Bool
	time.AfterFunc(150*time.Millisecond, func() { ready.Store(true) })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	app := helperApp(t, "launcher", "exit", "0")
	app.Ready = &config.Ready{
		HTTP:     &config.ReadyHTTP{URL: config.StringList{srv.URL}},
		Timeout:  config.Duration(5 * time.Second),
		Interval: config.Duration(20 * time.Millisecond),
	}
	h := newHarness(t, config.Apps{app}, Options{})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatalf("launcher that exits 0 should wait for readiness: %v", err)
	}
	if !ready.Load() {
		t.Fatal("Start returned before the app was ready")
	}
}

func TestNonZeroExitStillFailsFast(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	app := helperApp(t, "broken", "exit", "3")
	app.Ready = &config.Ready{
		HTTP:     &config.ReadyHTTP{URL: config.StringList{srv.URL}},
		Timeout:  config.Duration(10 * time.Second),
		Interval: config.Duration(20 * time.Millisecond),
	}
	h := newHarness(t, config.Apps{app}, Options{})
	start := time.Now()
	err := h.Start(t.Context(), nil)
	_ = mustCode(t, err, CodeExitedEarly)
	if time.Since(start) > 3*time.Second {
		t.Fatalf("non-zero exit must fail fast, took %s", time.Since(start))
	}
}
