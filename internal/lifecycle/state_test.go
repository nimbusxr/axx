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
	if err := Reap(stateFile, stdout, stderr); err != nil {
		t.Fatalf("Reap: %v", err)
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
	if err := writeState(stateFile, runState{Version: stateVersion, PID: 1, Apps: []stateApp{stale}}); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, config.Apps{helperApp(t, "api", "sleep")}, Options{StateFile: stateFile})
	if err := h.Start(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if st, err := readState(stateFile); err != nil || len(st.Apps) != 2 {
		t.Fatalf("state while running = %+v, %v", st, err)
	}
	if !strings.Contains(h.logs.String(), "earlier run") {
		t.Errorf("no warning about the earlier run:\n%s", h.logs)
	}
	if err := h.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	st, err := readState(stateFile)
	if err != nil || len(st.Apps) != 1 || st.Apps[0].Name != "old" {
		t.Fatalf("state after Stop = %+v, %v; want only the earlier run's app", st, err)
	}
	if err := Reap(stateFile, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := readLines(t, events); !slices.Equal(got, []string{"old cleanup"}) {
		t.Errorf("events %q", got)
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
	if err := Reap(filepath.Join(dir, "missing.json"), nil, nil); err != nil {
		t.Errorf("Reap of a missing file: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	ae := mustCode(t, Reap(bad, nil, nil), CodeStateFile)
	if !strings.Contains(ae.Hint, bad) {
		t.Errorf("hint = %q", ae.Hint)
	}
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := writeState(corrupt, runState{Version: stateVersion, Apps: []stateApp{{Name: "x", PID: 1, PGID: 1}}}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		// Never kill(-1): a group id of 1 is refused.
		wantCode(t, Reap(corrupt, nil, nil), CodeStateFile)
	}
}
