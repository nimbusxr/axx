// Package verify holds the verifier's checks that need no running system:
// which files the agent changed, what axx.yaml defines, and whether custom
// steps are real. The runs against the correct and the mutant apps live in
// cmd/evals-verify.
package verify

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/goccy/go-yaml"

	"github.com/nimbusxr/axx/evals/internal/manifest"
	"github.com/nimbusxr/axx/evals/internal/spec"
)

// Finding is one failed rule.
type Finding struct {
	Rule    string `json:"rule"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

// Static is the result of the static checks.
type Static struct {
	Changes  []manifest.Change `json:"changes"`
	Findings []Finding         `json:"findings"`
}

// ProbePattern matches code that tries to find out which variant of the app
// is running instead of testing its behavior.
var ProbePattern = regexp.MustCompile(`(?i)evals_mutant|\bmutants?\b|/proc/|/opt/evals|/logs/verifier`)

// sourceExt are files whose content can run during a test.
var sourceExt = map[string]bool{
	".ts": true, ".mts": true, ".cts": true, ".js": true, ".mjs": true, ".cjs": true,
	".py": true, ".sh": true, ".bash": true, ".zsh": true, ".rb": true, ".pl": true, ".go": true,
}

// CheckStatic runs every static rule for the project in ws.
func CheckStatic(ws string, v *spec.Verify, initial manifest.Manifest, initialConfig []byte) (*Static, error) {
	current, err := manifest.Scan(ws)
	if err != nil {
		return nil, err
	}
	st := &Static{Changes: manifest.Diff(initial, current), Findings: []Finding{}}
	add := func(rule, p, format string, args ...any) {
		st.Findings = append(st.Findings, Finding{Rule: rule, Path: p, Message: fmt.Sprintf(format, args...)})
	}

	for _, c := range st.Changes {
		if !v.AllowsPath(c.Path) {
			add("allowed-paths", c.Path, "%s outside the paths this task may change (%s)", c.Kind, strings.Join(v.Allowed, ", "))
		}
	}
	for _, g := range v.Required {
		if !anyMatch(current, g) {
			add("required", g, "no file matches %s", g)
		}
	}
	for _, g := range v.Absent {
		for p := range current {
			if ok, _ := doublestar.Match(g, p); ok {
				add("absent", p, "%s must not exist (matches %s)", p, g)
			}
		}
	}
	for _, r := range v.MustNotContain {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return nil, fmt.Errorf("must_not_contain %q: %w", r.Pattern, err)
		}
		for p := range current {
			if ok, _ := doublestar.Match(r.Path, p); !ok {
				continue
			}
			b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(p)))
			if err == nil && re.Match(b) {
				add("must-not-contain", p, "%s still contains %q: %s", p, r.Pattern, r.Reason)
			}
		}
	}

	// Readability of the features and probes in code the agent wrote or changed.
	for _, c := range st.Changes {
		if c.Kind == "deleted" {
			continue
		}
		ext := path.Ext(c.Path)
		if ext != ".feature" && !sourceExt[ext] {
			continue
		}
		b, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(c.Path)))
		if err != nil {
			continue
		}
		if ext == ".feature" {
			st.Findings = append(st.Findings, CheckFeature(c.Path, b)...)
			continue
		}
		if m := ProbePattern.Find(b); m != nil {
			add("probe", c.Path, "code refers to %q: tests must observe the service's behavior, not the evaluation harness", string(m))
		}
	}

	cfgPath := filepath.Join(ws, "axx.yaml")
	cfgBytes, err := os.ReadFile(cfgPath)
	if err != nil {
		return st, nil //nolint:nilerr // no axx.yaml: nothing to check (tasks that need one say so in required)
	}
	var cfg map[string]any
	if err := yaml.Unmarshal(cfgBytes, &cfg); err != nil {
		add("config", "axx.yaml", "cannot parse axx.yaml: %v", err)
		return st, nil
	}
	anyKey := len(v.ConfigKeys) == 1 && v.ConfigKeys[0] == "*"
	if initialConfig != nil && !anyKey {
		var before map[string]any
		if err := yaml.Unmarshal(initialConfig, &before); err == nil {
			for _, k := range changedKeys(before, cfg) {
				switch {
				case contains(v.ConfigKeys, k):
				case len(v.ConfigKeys) == 0:
					add("config-keys", "axx.yaml", "axx.yaml %q changed; this task does not change axx.yaml", k)
				default:
					add("config-keys", "axx.yaml", "axx.yaml %q changed; this task may only change %s", k, strings.Join(v.ConfigKeys, ", "))
				}
			}
		}
	}
	return st, nil
}

func anyMatch(m manifest.Manifest, g string) bool {
	for p := range m {
		if ok, _ := doublestar.Match(g, p); ok {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

func changedKeys(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var out []string
	for k := range keys {
		if !reflect.DeepEqual(a[k], b[k]) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
