// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/filelock"
)

// ranProject is a project runs have left things in.
func ranProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	writeFiles(t, dir, map[string]string{
		"axx.yaml":                   "run:\n  reporters:\n    - pretty\n    - junit: build/axx/junit.xml\n    - html: ../report-outside.html\n",
		"features/a.feature":         "Feature: a\n\n  Scenario: b\n",
		".axx/logs/services.log":     "ready\n",
		".axx/web/videos/a.mp4":      "video",
		".axx/demo/a/take.json":      "{}",
		".axx/properties/api":        "api.port=1\n",
		".axx/run/state.json":        "{}",
		".axx/host.json":             "{}",
		"build/axx/junit.xml":        "<testsuites/>",
		"build/axx/kept-by-hand.txt": "mine",
	})
	writeFiles(t, filepath.Dir(dir), map[string]string{"report-outside.html": "<html/>"})
	return dir
}

func present(dir string, names ...string) (there, gone []string) {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(n))); err == nil {
			there = append(there, n)
		} else {
			gone = append(gone, n)
		}
	}
	return there, gone
}

func TestCleanRemovesWhatRunsLeft(t *testing.T) {
	dir := ranProject(t)
	left := []string{".axx/logs", ".axx/web", ".axx/demo", ".axx/properties", "build/axx/junit.xml"}
	stays := []string{".axx/run/state.json", ".axx/host.json", "build/axx/kept-by-hand.txt", "../report-outside.html", "axx.yaml", "features/a.feature"}

	out, stderr, code := run(t, "clean", "--dry-run")
	if code != int(exitcode.OK) || !strings.Contains(out, "would remove") || !strings.Contains(out, ".axx/web") || !strings.Contains(out, "build/axx/junit.xml") {
		t.Fatalf("dry run, exit %d:\n%s%s", code, out, stderr)
	}
	if _, gone := present(dir, left...); len(gone) > 0 {
		t.Fatalf("a dry run removed %v", gone)
	}

	out, stderr, code = run(t, "clean", "--json")
	if code != int(exitcode.OK) {
		t.Fatalf("exit %d:\n%s%s", code, out, stderr)
	}
	data := envelope(t, out).Data.(map[string]any)
	if got := len(data["removed"].([]any)); got != len(left) || data["bytes"].(float64) == 0 || data["dryRun"] != false {
		t.Errorf("removed %d things, want %d: %s", got, len(left), out)
	}
	if there, _ := present(dir, left...); len(there) > 0 {
		t.Errorf("still there: %v", there)
	}
	if _, gone := present(dir, stays...); len(gone) > 0 {
		t.Errorf("removed what is not a run's to leave, or says what is running: %v", gone)
	}

	out, _, code = run(t, "clean")
	if code != int(exitcode.OK) || !strings.Contains(out, "nothing to remove") {
		t.Errorf("a second clean, exit %d: %s", code, out)
	}
}

func TestCleanRefusesWhileARunIsGoingOn(t *testing.T) {
	dir := ranProject(t)
	// A run holds the turn with the project's services for as long as it goes on.
	end, ok, err := filelock.TryLock(filepath.Join(dir, ".axx", "run", "turn.lock"))
	if err != nil || !ok {
		t.Fatalf("lock: %v %v", ok, err)
	}
	_, stderr, code := run(t, "clean")
	if code != int(exitcode.Environment) || !strings.Contains(stderr, codeProjectInUse) || !strings.Contains(stderr, "a run of this project is going on") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if _, gone := present(dir, ".axx/logs/services.log", "build/axx/junit.xml"); len(gone) > 0 {
		t.Fatalf("removed under a run: %v", gone)
	}
	// What would go can still be asked.
	if out, _, code := run(t, "clean", "--dry-run"); code != int(exitcode.OK) || !strings.Contains(out, "would remove") {
		t.Errorf("dry run, exit %d: %s", code, out)
	}
	end()
	if _, stderr, code := run(t, "clean"); code != int(exitcode.OK) {
		t.Errorf("after the run, exit %d:\n%s", code, stderr)
	}
}

func TestCleanWhereNoRunWas(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	out, _, code := run(t, "clean")
	if code != int(exitcode.OK) || !strings.Contains(out, "not an axx project") {
		t.Errorf("exit %d: %s", code, out)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("clean made %v", entries)
	}
}
