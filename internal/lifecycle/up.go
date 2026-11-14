package lifecycle

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/filelock"
)

// UpFile is where the supervisor `axx up` starts records the services it
// keeps running for the project in dir.
func UpFile(dir string) string { return filepath.Join(dir, ".axx", "run", "up.json") }

// UpLock is the file that supervisor holds a lock on for as long as it runs.
// That is how the project's other commands tell that it runs: the lock goes
// with the process, and whether it is held is a question of the file, which
// has an answer in a sandbox too, where a run may not signal a process
// outside it (macOS) or cannot see one (a Linux process namespace).
func UpLock(dir string) string { return filepath.Join(dir, ".axx", "run", "up.lock") }

// HoldUp takes the supervisor's lock, which it holds until its process ends.
// It waits out a command that is looking at the lock just now; another
// supervisor's keeps it, until ctx ends.
func HoldUp(ctx context.Context, dir string) error {
	release, err := filelock.Lock(ctx, UpLock(dir), nil)
	if err != nil {
		return err
	}
	heldUp = release
	return nil
}

// heldUp keeps the supervisor's lock for as long as its process runs: a
// lock nothing refers to goes when its file is collected.
var heldUp func()

// upRecord is what the supervisor records in UpFile.
type upRecord struct {
	PID      int      `json:"pid"`
	Services []string `json:"services"`
	// Locked says the supervisor holds UpLock: one of an axx before the
	// lock does not, and is told by its process.
	Locked bool `json:"locked"`
}

func readUp(dir string) (upRecord, bool) {
	var r upRecord
	b, err := os.ReadFile(UpFile(dir))
	if err != nil || json.Unmarshal(b, &r) != nil {
		return r, false
	}
	return r, true
}

// UpRunning reports whether that supervisor still runs.
func UpRunning(dir string) bool {
	r, ok := readUp(dir)
	return ok && SupervisorRuns(dir, r.PID, r.Locked)
}

// SupervisorRuns reports whether the supervisor UpFile records runs: by its
// lock when it holds one (locked), and by its process otherwise, or when the
// lock cannot be read.
func SupervisorRuns(dir string, pid int, locked bool) bool {
	if locked {
		if held, err := filelock.Held(UpLock(dir)); err == nil {
			return held
		}
	}
	return ProcessAlive(pid)
}

// UpTold is what the services `axx up` keeps running in dir told axx: none
// when no supervisor runs.
func UpTold(dir string) ([]ToldValues, error) {
	services, ok := upServices(dir)
	if !ok {
		return nil, nil
	}
	files := make([]Told, len(services))
	for i, name := range services {
		files[i] = Told{Who: "service " + name, Service: name, Path: PropertiesFile(PropertiesDir(dir), name)}
	}
	return ReadTold(files)
}

// upServices are the services the supervisor of `axx up` in dir keeps, while
// it runs.
func upServices(dir string) ([]string, bool) {
	r, ok := readUp(dir)
	if !ok || !SupervisorRuns(dir, r.PID, r.Locked) {
		return nil, false
	}
	return r.Services, true
}
