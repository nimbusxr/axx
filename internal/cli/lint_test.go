package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/lint"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// lintProject creates a suite whose seeds collide on "mission-1".
func lintProject(t *testing.T, mode string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{
		"axx.yaml": `version: 1
lint:
  include: [axx-lint.generated.yaml]
  config: {mode: ` + mode + `}
  rules:
    - name: Seed IDs
      filePatterns: ["seeds/*.yaml"]
      regex: '^\s+-?\s*id:\s*"([^"]+)"'
      validation: cross-file-unique
`,
		"axx-packs.yaml":          "packs: [sql]\n",
		"axx-lint.generated.yaml": "rules:\n  - {name: event ids, filePatterns: [\"kafka/*.json\"], type: jsonpath, jsonPath: id, validation: cross-file-unique}\n",
		"seeds/a.yaml":            "t:\n  - id: \"mission-1\"\n  - id: \"mission-2\"\n",
		"seeds/b.yaml":            "t:\n  - id: \"mission-1\"\n",
		"kafka/e.json":            `{"id": "e-1"}`,
		"features/f.feature":      "Feature: f\n  Scenario: s\n    Then the 2nd selection has 1 row\n",
	})
	t.Chdir(dir)
	return dir
}

func TestLintCommand(t *testing.T) {
	dir := lintProject(t, "error")

	out, stderr, code := run(t, "lint", "--compact=false")
	if code != int(exitcode.Undefined) {
		t.Fatalf("violations in error mode exit 3, got %d\n%s%s", code, out, stderr)
	}
	for _, want := range []string{
		"FAIL Seed IDs (cross-file-unique, 2 files): 1 duplicate value",
		`seeds/a.yaml:2:10  - id: "mission-1"`,
		"ok   event ids (cross-file-unique, 1 file)",
		"warn SQL selection and trigger ordinals",
		"features/f.feature:3",
		"axx lint: 3 rules, 4 files: 1 error, 1 warning",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output lacks %q:\n%s", want, out)
		}
	}

	out, _, code = run(t, "lint", "--json")
	var env struct {
		Envelope
		Data lint.Report `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if code != 3 || env.OK || env.Command != "axx lint" || env.Data.Summary.Errors != 1 || env.Data.Rules[0].Findings[0].Value != "mission-1" {
		t.Fatalf("json envelope: exit %d %+v", code, env)
	}

	// --mode warn reports but passes; paths limit the report.
	if _, _, code := run(t, "lint", "--mode", "warn"); code != 0 {
		t.Errorf("--mode warn: exit %d", code)
	}
	if out, _, code := run(t, "lint", "kafka"); code != 0 || !strings.HasSuffix(out, ": ok\n") {
		t.Errorf("lint kafka reports only findings touching kafka/: exit %d\n%s", code, out)
	}
	if out, _, code := run(t, "lint", "seeds/b.yaml"); code != 3 || !strings.Contains(out, "1 error, 0 warnings") {
		t.Errorf("lint seeds/b.yaml: exit %d\n%s", code, out)
	}

	// Machine formats: to stdout, or to a file relative to axx.yaml (then
	// the human report still goes to stdout).
	out, _, code = run(t, "lint", "-f", "github", "-f", "sarif:build/lint.sarif", "-f", "junit:build/lint.xml", "-f", "json:build/lint.json")
	if code != 3 || !strings.Contains(out, "::error file=seeds/a.yaml,line=2,col=10,title=axx lint%3A Seed IDs::") {
		t.Errorf("github output (exit %d):\n%s", code, out)
	}
	if strings.Contains(out, "FAIL Seed IDs") {
		t.Errorf("stdout carries only the github format:\n%s", out)
	}
	for _, f := range []string{"build/lint.sarif", "build/lint.xml", "build/lint.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || len(b) == 0 {
			t.Errorf("%s not written: %v", f, err)
		}
	}
	out, _, _ = run(t, "lint", "-f", "sarif:build/only.sarif")
	if !strings.Contains(out, "axx lint: 3 rules") {
		t.Errorf("with only file outputs, the human report goes to stdout:\n%s", out)
	}
}

func TestLintCommandWarnMode(t *testing.T) {
	lintProject(t, "warn")
	out, _, code := run(t, "lint", "--compact=false")
	if code != 0 || !strings.Contains(out, "warn Seed IDs (cross-file-unique, warn mode, 2 files)") {
		t.Fatalf("warn mode passes (exit %d):\n%s", code, out)
	}
	if _, _, code := run(t, "lint", "--mode", "error"); code != 3 {
		t.Errorf("--mode error overrides the configured warn mode: exit %d", code)
	}
}

func TestLintCommandErrors(t *testing.T) {
	dir := lintProject(t, "error")
	for _, tt := range []struct {
		args []string
		code string
	}{
		{[]string{"lint", "--format", "bogus"}, "AXX-E0805"},
		{[]string{"lint", "--mode", "strict"}, "AXX-E0805"},
	} {
		_, stderr, code := run(t, tt.args...)
		if code != int(exitcode.Usage) || !strings.Contains(stderr, tt.code) {
			t.Errorf("%v: exit %d, stderr %q", tt.args, code, stderr)
		}
	}

	writeFiles(t, dir, map[string]string{"axx-lint.generated.yaml": "rules:\n  - {name: bad, filePatterns: [x], regex: '(x'}\n"})
	_, stderr, code := run(t, "lint")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "error[AXX-E0804]: axx-lint.generated.yaml:2:") || !strings.Contains(stderr, "hint:") {
		t.Errorf("config errors exit 2 with a code, location and hint: exit %d\n%s", code, stderr)
	}
	out, _, _ := run(t, "lint", "--json")
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil || env.OK || len(env.Errors) != 1 || env.Errors[0].Code != "AXX-E0804" || env.Errors[0].Location == nil {
		t.Errorf("json error envelope: %v %s", err, out)
	}

	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\nlint:\n  rules:\n    - {name: r, filePatterns: [x], regex: '(x)', validation: unique}\n"})
	if _, stderr, code := run(t, "lint"); code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0102") || !strings.Contains(stderr, "axx.yaml:4:") {
		t.Errorf("schema errors in the lint section: exit %d\n%s", code, stderr)
	}
}

func TestValidateAndDoctorShowLint(t *testing.T) {
	dir := lintProject(t, "error")
	out, _, code := run(t, "validate", "--json")
	var env struct {
		Data ValidationReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil || code != 0 {
		t.Fatalf("validate: exit %d %v\n%s", code, err, out)
	}
	if w := env.Data.Warnings; len(w) != 1 || w[0].Location != "features/f.feature:3" || w[0].Text != "the 2nd selection has 1 row" || !strings.Contains(w[0].Message, "AXX-E0830") {
		t.Errorf("validate warnings: %+v", env.Data.Warnings)
	}
	out, _, _ = run(t, "validate")
	if !strings.Contains(out, "features/f.feature:3: warning: the 2nd selection has 1 row") || !strings.Contains(out, ": ok, 1 warning") {
		t.Errorf("validate human output:\n%s", out)
	}

	doctorCheck := func() DoctorCheck {
		t.Helper()
		out, _, _ := run(t, "doctor", "--json")
		var env struct {
			Data DoctorReport `json:"data"`
		}
		if err := json.Unmarshal([]byte(out), &env); err != nil {
			t.Fatalf("doctor: %v\n%s", err, out)
		}
		for _, c := range env.Data.Checks {
			if c.Name == "lint rules" {
				return c
			}
		}
		t.Fatalf("doctor has no lint check:\n%s", out)
		return DoctorCheck{}
	}
	if c := doctorCheck(); c.Status != "ok" || c.Detail != "2 rules" {
		t.Errorf("doctor lint check: %+v", c)
	}
	writeFiles(t, dir, map[string]string{"axx-lint.generated.yaml": "rules: [\n"})
	if c := doctorCheck(); c.Status != "warn" || !strings.Contains(c.Detail, "axx-lint.generated.yaml") || c.Hint == "" {
		t.Errorf("doctor notes an invalid lint config: %+v", c)
	}
}

func TestLintWithoutLintSection(t *testing.T) {
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\n"})
	t.Chdir(dir)
	out, _, code := run(t, "lint")
	if code != 0 || !strings.Contains(out, "no test-data isolation rules configured") {
		t.Errorf("exit %d\n%s", code, out)
	}
}

// The example suite must lint cleanly (generated fixture outputs such as
// kafka/*.json may be absent: a pattern that matches nothing is fine).
func TestLintExampleSuite(t *testing.T) {
	dir, err := filepath.Abs(filepath.Join("..", "..", "examples", "parcels", "acceptance"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	out, stderr, code := run(t, "lint", "--compact=false")
	if code != 0 {
		t.Fatalf("axx lint on the example: exit %d\n%s%s", code, out, stderr)
	}
	if !strings.Contains(out, "ok   Parcel references in seeds") || strings.Contains(out, "warn ") {
		t.Errorf("unexpected report:\n%s", out)
	}
}
