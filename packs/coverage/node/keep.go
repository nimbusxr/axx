package coveragenode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	coveragecore "github.com/nimbusxr/axx/packs/coverage/core"
)

// takeWait is how long the service's processes have to take their coverage
// when a run asks; takeSettle, how long the others have once one has (the
// preload looks every 200ms).
var (
	takeWait   = 10 * time.Second
	takeSettle = 500 * time.Millisecond
)

// baseFolder keeps, from the reset, every function of the service's
// scripts: a take after a reset lists only those that ran since, so the
// run's coverage is the take merged with these, their counts zero.
const baseFolder = "v8-base"

// Reset takes what ran so far: it is dropped, and its functions, their
// counts zero, are the base the run's take adds to.
func (nodeRuntime) Reset(ctx context.Context, call coveragecore.LaunchCall) error {
	dir := call.Service.Dir
	if err := take(ctx, dir); err != nil {
		return err
	}
	base := filepath.Join(dir, baseFolder)
	if err := os.RemoveAll(base); err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(dir, v8Folder, "coverage-*.json"))
	for i, f := range files {
		b, err := os.ReadFile(f)
		if tail(dir, f, err) {
			_ = os.Remove(f) // what ran as a process of it ended, before the reset
			continue
		}
		if err != nil {
			return err
		}
		zero, err := zeroed(b)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		if err := os.WriteFile(filepath.Join(base, fmt.Sprintf("coverage-base-%d.json", i)), zero, 0o644); err != nil {
			return err
		}
		_ = os.Remove(f)
	}
	return nil
}

// Take writes what ran since the reset into into, as a stopped service's
// folder has it: the take, and the base.
func (nodeRuntime) Take(ctx context.Context, call coveragecore.LaunchCall, into string) error {
	dir := call.Service.Dir
	if err := take(ctx, dir); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(into, v8Folder), 0o755); err != nil {
		return err
	}
	files, _ := filepath.Glob(filepath.Join(dir, v8Folder, "coverage-*.json"))
	for _, f := range files {
		if r, err := os.Open(f); tail(dir, f, err) {
			_ = os.Remove(f) // what ran as a process of it ended, after its take, which is here
			continue
		} else if err == nil {
			_ = r.Close()
		}
		if err := os.Rename(f, filepath.Join(into, v8Folder, filepath.Base(f))); err != nil {
			return err
		}
	}
	base, _ := filepath.Glob(filepath.Join(dir, baseFolder, "coverage-*.json"))
	for _, f := range base {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(into, v8Folder, filepath.Base(f)), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// zeroed is a file of V8's coverage with every count zero: its functions'
// places, for a base.
func zeroed(b []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	scripts, _ := doc["result"].([]any)
	for _, sc := range scripts {
		fns, _ := sc.(map[string]any)["functions"].([]any)
		for _, fn := range fns {
			ranges, _ := fn.(map[string]any)["ranges"].([]any)
			for _, r := range ranges {
				if m, ok := r.(map[string]any); ok {
					m["count"] = 0
				}
			}
		}
	}
	return json.Marshal(doc)
}

// take has the service's Node processes write V8's coverage: the preload
// takes it when the trigger is newer than the last it saw, and says so in a
// file of its own, which take waits for (none is left from before).
func take(ctx context.Context, dir string) error {
	old, _ := filepath.Glob(filepath.Join(dir, "taken-*"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	trigger := filepath.Join(dir, "take")
	now := time.Now()
	if err := os.WriteFile(trigger, nil, 0o644); err != nil {
		return err
	}
	if err := os.Chtimes(trigger, now, now); err != nil {
		return err
	}
	deadline := time.Now().Add(takeWait)
	for {
		if taken, _ := filepath.Glob(filepath.Join(dir, "taken-*")); len(taken) > 0 {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the service's Node processes took no coverage within %s: do they run with coverage-node's preload (axx up --coverage)?", takeWait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(takeSettle):
	}
	return nil
}
