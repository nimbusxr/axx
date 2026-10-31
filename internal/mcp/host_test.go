package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/host"
)

// With an axx host serving the project, the agent's session has services of
// its own from it, started clean; a run ends them first, so that it starts
// clean too.
func TestTheSessionHasItsServicesFromTheHost(t *testing.T) {
	engine.Register("./shelf", shelf{})
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cleanups := filepath.Join(dir, "cleanups")
	q := func(s string) string { b, _ := json.Marshal(s); return string(b) }
	for name, content := range map[string]string{
		"axx.yaml": "services:\n  api:\n" +
			"    command: [" + q(exe) + ", parcels, api, ready]\n" +
			"    cleanup: [" + q(exe) + ", append, " + q(cleanups) + ", api]\n" +
			"    ready:\n      log: parcels api ready\n      timeout: 20s\n",
		"axx-packs.yaml": "packs: [./shelf]\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	var runs []host.Event
	srv, err := host.New(context.Background(), host.Options{
		Load:     config.LoadOptions{WorkDir: dir},
		Approver: host.NewTerminal(strings.NewReader(""), &strings.Builder{}, false, false),
		Events: func(e host.Event) {
			if e.Event == "run" {
				mu.Lock()
				runs = append(runs, e)
				mu.Unlock()
			}
		},
		Approvals: "-",
		Env:       append(os.Environ(), helperEnv+"=1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = srv.Serve(ctx)
	}()
	t.Cleanup(func() { cancel(); <-served })
	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if a, _ := host.Find(dir); a != nil {
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatal("the host did not announce itself")
		}
	}
	states := func() []string {
		mu.Lock()
		defer mu.Unlock()
		var out []string
		for _, e := range runs {
			out = append(out, e.Command+": "+e.State)
		}
		return out
	}

	mcpSrv, s := newServer(Options{WorkDir: dir, Exe: filepath.Join(dir, "no-axx-here")})
	st, ct := sdk.NewInMemoryTransports()
	go func() { _ = mcpSrv.Run(context.Background(), st) }()
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	call(t, cs, "steps_try", map[string]any{"steps": "Given parcel PX-4101 is put on the shelf"})
	if got := strings.Join(states(), "\n"); !strings.Contains(got, "axx mcp: steps_try: started") {
		t.Fatalf("the session's services:\n%s", got)
	}
	if b, _ := os.ReadFile(cleanups); len(b) != 0 {
		t.Fatalf("cleaned up while the session had them: %q", b)
	}
	if log, _ := os.ReadFile(filepath.Join(dir, ".axx", "logs", "services.log")); !strings.Contains(string(log), "[api] parcels api ready") {
		t.Errorf("services.log: %q", log)
	}

	// A run ends the session and its services first.
	_, _, _ = s.scenariosRun(context.Background(), nil, scenariosRunIn{})
	if b, _ := os.ReadFile(cleanups); strings.TrimSpace(string(b)) != "api" {
		t.Errorf("cleanups after scenarios_run: %q", b)
	}
	if s.hostLeased() {
		t.Error("the session still has services")
	}
	// The next steps start a fresh session, with fresh services.
	call(t, cs, "steps_try", map[string]any{"steps": "Given parcel PX-4102 is put on the shelf"})
	if got := strings.Count(strings.Join(states(), "\n"), "axx mcp: steps_try: started"); got != 2 {
		t.Errorf("services started %d times:\n%s", got, strings.Join(states(), "\n"))
	}
	if out := call(t, cs, "shelf_list", map[string]any{}); out["parcels"] == nil || len(out["parcels"].([]any)) != 1 {
		t.Errorf("a fresh scenario: %v", out)
	}
}
