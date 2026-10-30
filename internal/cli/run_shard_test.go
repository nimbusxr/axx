package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// Runners that share a run with --shard run every scenario it selects once between them: every
// nth, in their files' and lines' order.
func TestRunShards(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var b strings.Builder
	b.WriteString("Feature: Health\n")
	for _, n := range []string{"one", "two", "three"} {
		b.WriteString("\n  Scenario: health " + n + "\n    Given a GET request to /health\n")
	}
	if err := os.MkdirAll(filepath.Join(dir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "features", "health.feature"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// The project has no packs: its step is undefined, and the dry run says which scenarios it ran.
	for shard, want := range map[string][]string{"1/2": {"one", "three"}, "2/2": {"two"}} {
		out, _, _ := run(t, "run", "--dry-run", "--shard", shard, "--format", "progress")
		var got []string
		for _, n := range []string{"one", "two", "three"} {
			if strings.Contains(out, "Scenario: health "+n+" ") {
				got = append(got, n)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("--shard %s runs %v, not %v:\n%s", shard, got, want, out)
		}
	}
	_, stderr, code := run(t, "run", "--dry-run", "--shard", "3/2")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0206") {
		t.Errorf("--shard 3/2: exit %d, %s", code, stderr)
	}
}
