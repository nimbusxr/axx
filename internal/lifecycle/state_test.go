package lifecycle

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/config"
)

func TestStateFileAndReap(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, ".axx", "run", "state.json")
	events := filepath.Join(dir, "events")
	h := newHarness(t, config.Apps{recorder(t, events, "db"), recorder(t, events, "api")}, Options{StateFile: stateFile})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	st, err := readState(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if st.PID != os.Getpid() || len(st.Apps) != 2 {
		t.Fatalf("state = %+v", st)
	}
	for i, name := range []string{"db", "api"} {
		sa := st.Apps[i]
		if sa.Name != name || sa.PID != h.pidOf(t, name) || sa.PGID == 0 || sa.StartedAt.IsZero() ||
			sa.Dir != h.dir || !slices.Contains(sa.Cleanup, name+" cleanup") || sa.Env[helperEnv] != "1" {
			t.Errorf("state entry %d = %+v", i, sa)
		}
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(stateFile); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("state file mode = %v, %v; want 0600", info.Mode(), err)
		}
	}

	// Simulate `axx down` after this run was killed.
	stdout, stderr := &buffer{}, &buffer{}
	reaped, err := Reap(stateFile, stdout, stderr)
	if err != nil {
		t.Fatalf("Reap: %v", err)
	}
	if !slices.Equal(reaped, []string{"api", "db"}) {
		t.Errorf("reaped %q", reaped)
	}
	for _, name := range []string{"db", "api"} {
		if pid := h.pidOf(t, name); processAlive(pid) {
			t.Errorf("%s (pid %d) survived Reap", name, pid)
		}
	}
	if got, want := only(readLines(t, events), "cleanup"), []string{"api", "db"}; !slices.Equal(got, want) {
		t.Errorf("cleanups %q, want %q", got, want)
	}
	if out := stdout.String(); !strings.Contains(out, "stopping app api") || !strings.Contains(out, "running cleanup of app db") {
		t.Errorf("Reap output:\n%s", out)
	}
	if _, err := os.Stat(stateFile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state file still exists after Reap: %v", err)
	}
}

func TestStopRemovesStateFile(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{StateFile: stateFile})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("state file missing while running: %v", err)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stateFile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state file still exists after Stop: %v", err)
	}
}

func TestStateFileKeepsEarlierRun(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	events := filepath.Join(dir, "events")
	// An earlier run was killed; its app is gone but its cleanup never ran.
	stale := stateApp{
		Name: "old", PID: 1 << 22, PGID: 1 << 22, Dir: dir, Cleanup: helper(t, "append", events, "old cleanup").Argv,
		Env: helperEnvVars(),
	}
	if err := writeState(stateFile, runState{Version: stateVersion, PID: 1 << 22, Apps: []stateApp{stale}}); err != nil {
		t.Fatal(err)
	}
	// A run does not start from what the earlier run left.
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{StateFile: stateFile})
	ae := mustCode(t, h.Start(t.Context(), nil), CodeNotCleanedUp)
	if !strings.Contains(ae.Message, "the cleanup of app old never ran") || !strings.Contains(ae.Hint, "axx down") {
		t.Errorf("error = %v (hint %q)", ae, ae.Hint)
	}
	if st, err := readState(stateFile); err != nil || len(st.Apps) != 1 {
		t.Fatalf("state after the refusal = %+v, %v", st, err)
	}
	reaped, err := Reap(stateFile, nil, nil)
	if err != nil || !slices.Equal(reaped, []string{"old"}) {
		t.Fatalf("Reap = %q, %v", reaped, err)
	}
	if got := readLines(t, events); !slices.Equal(got, []string{"old cleanup"}) {
		t.Errorf("events %q", got)
	}
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// A cleanup that fails, like `docker compose down` without access to
// Docker, stays in the state file: runs refuse to start apps, and `axx
// down` runs it again until it succeeds.
func TestAFailedCleanupIsKeptUntilItSucceeds(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	docker := filepath.Join(dir, "docker-access") // the cleanup fails until it exists
	db := logReady(helperApp(t, "db", "print", "ready"), "^ready$")
	db.Cleanup = helper(t, "exists", docker)
	h := newHarness(t, config.Apps{db}, Options{StateFile: stateFile})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	wantCode(t, h.Stop(t.Context()), CodeCleanupFailed)
	st, err := readState(stateFile)
	if err != nil || len(st.Apps) != 1 || st.Apps[0].Name != "db" || !st.Apps[0].CleanupFailed {
		t.Fatalf("state after the failed cleanup = %+v, %v", st, err)
	}
	apps, err := Status(stateFile)
	if err != nil || len(apps) != 1 || apps[0].State != AppNotCleaned || !apps[0].CleanupFailed || !strings.Contains(apps[0].Cleanup, "exists") {
		t.Fatalf("status = %+v, %v", apps, err)
	}

	next := newHarness(t, config.Apps{db}, Options{StateFile: stateFile})
	ae := mustCode(t, next.Start(t.Context(), nil), CodeNotCleanedUp)
	if !strings.Contains(ae.Message, "the cleanup of app db failed") {
		t.Errorf("error = %v", ae)
	}

	// Still no access: the cleanup fails again and stays.
	reaped, err := Reap(stateFile, nil, nil)
	wantCode(t, err, CodeCleanupFailed)
	if len(reaped) != 0 {
		t.Errorf("reaped %q", reaped)
	}
	if st, err := readState(stateFile); err != nil || len(st.Apps) != 1 || !st.Apps[0].CleanupFailed {
		t.Fatalf("state after the second failure = %+v, %v", st, err)
	}

	if err := os.WriteFile(docker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	reaped, err = Reap(stateFile, nil, nil)
	if err != nil || !slices.Equal(reaped, []string{"db"}) {
		t.Fatalf("Reap = %q, %v", reaped, err)
	}
	if _, err := os.Stat(stateFile); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("state file still exists once cleaned up: %v", err)
	}
	if err := next.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err := next.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStatusSaysWhatEarlierRunsLeft(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "state.json")
	gone, alive := 1<<22, os.Getpid()
	cleanup := []string{"docker", "compose", "down", "-v"}
	st := runState{Version: stateVersion, PID: gone, Apps: []stateApp{
		{Name: "api", PID: alive, Owner: alive},                 // its run is running
		{Name: "worker", PID: alive, Owner: gone},               // its run was killed
		{Name: "db", PID: gone, Owner: gone, Cleanup: cleanup},  // killed, cleanup never ran
		{Name: "broker", Cleanup: cleanup, CleanupFailed: true}, // cleanup failed
		{Name: "cache", PID: gone, Owner: gone},                 // gone, nothing to clean up
	}}
	if err := writeState(stateFile, st); err != nil {
		t.Fatal(err)
	}
	apps, err := Status(stateFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []AppStatus{
		{Name: "api", State: AppRunning, PID: alive},
		{Name: "worker", State: AppLeftOver, PID: alive},
		{Name: "db", State: AppNotCleaned, Cleanup: "docker compose down -v"},
		{Name: "broker", State: AppNotCleaned, Cleanup: "docker compose down -v", CleanupFailed: true},
	}
	if !slices.Equal(apps, want) {
		t.Errorf("status = %+v\nwant     %+v", apps, want)
	}
	if apps, err := Status(filepath.Join(t.TempDir(), "none.json")); err != nil || apps != nil {
		t.Errorf("status without a state file = %+v, %v", apps, err)
	}
}

// A run that reuses the apps `axx up` keeps running finds them in the state
// file; they are not leftovers, so it does not warn about them.
func TestStateFileQuietForAttachedApps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	up := stateApp{Name: "api", PID: 1 << 22, PGID: 1 << 22, Dir: dir}
	if err := writeState(stateFile, runState{Version: stateVersion, PID: 1, Apps: []stateApp{up}}); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{StateFile: stateFile, Attach: map[string]bool{"api": true}})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.logs.String(), "earlier run") {
		t.Errorf("warned about an app the run reuses:\n%s", h.logs)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestReapEdgeCases(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := Reap(filepath.Join(dir, "missing.json"), nil, nil); err != nil {
		t.Errorf("Reap of a missing file: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Reap(bad, nil, nil)
	ae := mustCode(t, err, CodeStateFile)
	if !strings.Contains(ae.Hint, bad) {
		t.Errorf("hint = %q", ae.Hint)
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := writeState(corrupt, runState{Version: stateVersion, Apps: []stateApp{{Name: "x", PID: 1, PGID: 1}}}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		// Never kill(-1): a group id of 1 is refused.
		_, err := Reap(corrupt, nil, nil)
		wantCode(t, err, CodeStateFile)
	}
}
