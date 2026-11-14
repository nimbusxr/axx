package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/filelock"
)

// gonePID is the id of a process that ended: what a supervisor's is to a
// command that cannot see it, in a sandbox with processes of its own.
func gonePID(t *testing.T) int {
	t.Helper()
	gone := exec.Command(os.Args[0], "-test.run=^$")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	return gone.Process.Pid
}

func writeUp(t *testing.T, dir string, record map[string]any) {
	t.Helper()
	b, _ := json.Marshal(record)
	if err := os.MkdirAll(filepath.Dir(UpFile(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UpFile(dir), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The supervisor of `axx up` runs for as long as it holds its lock, whatever
// a command can tell of its process: a run in a sandbox cannot signal it, or
// see it at all.
func TestTheSupervisorRunsWhileItHoldsItsLock(t *testing.T) {
	dir := t.TempDir()
	if UpRunning(dir) {
		t.Fatal("no up.json: running")
	}
	writeUp(t, dir, map[string]any{"pid": gonePID(t), "services": []string{"api"}, "locked": true})
	if UpRunning(dir) {
		t.Error("its lock was never taken: running")
	}
	release, ok, err := filelock.TryLock(UpLock(dir))
	if err != nil || !ok {
		t.Fatalf("the supervisor's lock: %v %v", ok, err)
	}
	if !UpRunning(dir) {
		t.Error("it holds its lock: not running")
	}
	if services, ok := upServices(dir); !ok || len(services) != 1 || services[0] != "api" {
		t.Errorf("its services: %v %v", services, ok)
	}
	release()
	// A supervisor that was killed: its record says a process of this id ran,
	// and one does (this one), but the lock is no one's.
	writeUp(t, dir, map[string]any{"pid": os.Getpid(), "services": []string{"api"}, "locked": true})
	if UpRunning(dir) {
		t.Error("its lock is no one's: running")
	}
	if _, ok := upServices(dir); ok {
		t.Error("its lock is no one's: it keeps services")
	}
}

// The supervisor of an axx before the lock recorded only its process, which
// tells whether it runs.
func TestASupervisorWithoutALockIsToldByItsProcess(t *testing.T) {
	dir := t.TempDir()
	writeUp(t, dir, map[string]any{"pid": os.Getpid(), "services": []string{"api"}})
	if !UpRunning(dir) {
		t.Error("its process runs: not running")
	}
	writeUp(t, dir, map[string]any{"pid": gonePID(t), "services": []string{"api"}})
	if UpRunning(dir) {
		t.Error("its process ended: running")
	}
}

// The supervisor's lock is its own until its process ends: nothing the
// supervisor does meanwhile lets go of it, a garbage collection included,
// and a second supervisor does not get it.
func TestTheSupervisorHoldsItsLockUntilItEnds(t *testing.T) {
	dir := t.TempDir()
	if err := HoldUp(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	defer func() { heldUp(); heldUp = nil }() // as its process ending does
	writeUp(t, dir, map[string]any{"pid": gonePID(t), "services": []string{"api"}, "locked": true})
	for range 3 {
		runtime.GC()
	}
	if !UpRunning(dir) {
		t.Error("the supervisor let go of its lock while it ran")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err := filelock.Lock(ctx, UpLock(dir), nil); err == nil {
		t.Error("a second supervisor got the lock")
	}
}
