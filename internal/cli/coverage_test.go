package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

// The minimums apply to a run of every scenario the configuration selects:
// a run that names some, or shares a run with others, says why it is
// partial.
func TestPartialRun(t *testing.T) {
	for name, c := range map[string]struct {
		f    runFlags
		args []string
		want string
	}{
		"every scenario": {},
		"paths":          {args: []string{"features/a.feature:3"}, want: "paths"},
		"tags":           {f: runFlags{tags: "@smoke"}, want: "--tags"},
		"names":          {f: runFlags{names: []string{"register"}}, want: "--name"},
		"a shard":        {f: runFlags{shard: "2/3"}, want: "axx coverage check"},
		"fail-fast":      {f: runFlags{failFast: true}, want: "--fail-fast"},
		"no-start":       {f: runFlags{noStart: true}, want: "--no-start"},
		"blank tags":     {f: runFlags{tags: "  "}},
	} {
		got := partialRun(&c.f, c.args)
		if (c.want == "") != (got == "") || !strings.Contains(got, c.want) {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// A run with coverage in a project without the coverage packs says which to
// add, before it starts anything.
func TestARunWithCoverageNeedsItsPacks(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for name, content := range map[string]string{
		"axx.yaml":                 "version: 1\nservices:\n  parcels:\n    command: ./never-started\n    coverage:\n      runtime: go\n",
		"features/parcels.feature": "Feature: Parcels\n\n  Scenario: A parcel\n    Given nothing\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, stderr, code := run(t, "run", "--coverage")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, codeNoCoveragePacks) || !strings.Contains(stderr, "axx pack add coverage-jvm") {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "starting parcels") {
		t.Errorf("it started a service first:\n%s", stderr)
	}
}
