package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/evals/internal/manifest"
	"github.com/nimbusxr/axx/evals/internal/spec"
)

func TestCheckStatic(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(ws, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := "version: 1\nrun:\n  paths: [features]\n"
	write("axx.yaml", cfg)
	write("docs/rules.md", "rules")
	initial, err := manifest.Scan(ws)
	if err != nil {
		t.Fatal(err)
	}
	v := &spec.Verify{
		Allowed: []string{"features/**", "steps/labels/**", "axx.yaml"}, ConfigKeys: []string{"lint"},
	}

	// The agent: a feature, a custom pack, a changed doc and extra config.
	write("features/a.feature", "Feature: A\n  Scenario: ok\n    Given x\n    Then y\n")
	write("docs/rules.md", "changed")
	write("steps/labels/pack.go", "package labels\n\nvar probe = os.Getenv(\"EVALS_MUTANT\")\n")
	write("axx.yaml", cfg+"apps:\n  api:\n    command: [\"true\"]\n")
	write(".claude/settings.json", "{}")
	write("node_modules/x/index.js", "x")

	st, err := CheckStatic(ws, v, initial, []byte(cfg))
	if err != nil {
		t.Fatal(err)
	}
	var rules []string
	for _, f := range st.Findings {
		rules = append(rules, f.Rule+":"+f.Path)
	}
	joined := strings.Join(rules, " ")
	for _, want := range []string{
		"allowed-paths:docs/rules.md",
		"probe:steps/labels/pack.go",
		"config-keys:axx.yaml",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing finding %s in %s", want, joined)
		}
	}
	for _, c := range st.Changes {
		if strings.HasPrefix(c.Path, ".claude") || strings.HasPrefix(c.Path, "node_modules") {
			t.Errorf("ignored path reported as a change: %s", c.Path)
		}
	}
}
