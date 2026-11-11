package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
)

const unformattedFeature = "Feature: Claims\n Scenario: A claim\n  Given  a thing\n   |a|bb|\n   |ccc|d|\n"

const formattedFeature = "Feature: Claims\n\n  Scenario: A claim\n    Given a thing\n      | a   | bb |\n      | ccc | d  |\n"

func fmtProject(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, dir, map[string]string{
		"axx.yaml":                "version: 1\nrun:\n  paths: [specs]\n",
		"specs/claims.feature":    unformattedFeature,
		"specs/done.feature":      formattedFeature,
		"other/claims.feature":    unformattedFeature,
		"specs/.hidden/x.feature": unformattedFeature,
	})
	t.Chdir(dir)
	return dir
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFmtCheckChangesNothingAndFails(t *testing.T) {
	dir := fmtProject(t)
	out, stderr, code := run(t, "fmt", "--check")
	if code != int(exitcode.Undefined) {
		t.Fatalf("exit %d, want 3\n%s%s", code, out, stderr)
	}
	if out != "specs/claims.feature is not formatted\n2 feature files, 1 not formatted: axx fmt formats them\n" {
		t.Errorf("output:\n%s", out)
	}
	if got := read(t, filepath.Join(dir, "specs/claims.feature")); got != unformattedFeature {
		t.Errorf("--check changed the file:\n%s", got)
	}
}

// Without paths, axx fmt formats the features axx run reads; with paths,
// those.
func TestFmtFormatsTheFeaturesRunReads(t *testing.T) {
	dir := fmtProject(t)
	out, stderr, code := run(t, "fmt")
	if code != 0 {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	if out != "formatted specs/claims.feature\n2 feature files, 1 formatted\n" {
		t.Errorf("output:\n%s", out)
	}
	if got := read(t, filepath.Join(dir, "specs/claims.feature")); got != formattedFeature {
		t.Errorf("formatted:\n%s", got)
	}
	if got := read(t, filepath.Join(dir, "other/claims.feature")); got != unformattedFeature {
		t.Error("a feature outside run.paths was formatted")
	}
	if got := read(t, filepath.Join(dir, "specs/.hidden/x.feature")); got != unformattedFeature {
		t.Error("a feature in a hidden folder was formatted")
	}
	if out, _, code := run(t, "fmt", "--check"); code != 0 || out != "2 feature files, all formatted\n" {
		t.Errorf("after formatting, --check: exit %d\n%s", code, out)
	}

	out, _, code = run(t, "fmt", "--json", "other")
	var env struct {
		Envelope
		Data FmtReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if code != 0 || !env.OK || env.Command != "axx fmt" || env.Data.Files != 1 ||
		len(env.Data.Changed) != 1 || env.Data.Changed[0] != "other/claims.feature" {
		t.Errorf("exit %d: %+v", code, env)
	}
}

// A feature that does not parse is named, with its line, and left alone;
// the others are formatted.
func TestFmtNamesAFeatureThatDoesNotParse(t *testing.T) {
	dir := fmtProject(t)
	broken := "Feature: Broken\n  Scenario: x\n    Given a\n      | a | b |\n      | c |\n"
	writeFiles(t, dir, map[string]string{"specs/broken.feature": broken})
	out, stderr, code := run(t, "fmt")
	if code != int(exitcode.Usage) {
		t.Fatalf("exit %d, want 2\n%s%s", code, out, stderr)
	}
	if !strings.Contains(stderr, "error[AXX-E0200]: specs/broken.feature:5:7: inconsistent cell count within the table") {
		t.Errorf("stderr:\n%s", stderr)
	}
	if read(t, filepath.Join(dir, "specs/broken.feature")) != broken {
		t.Error("the feature that does not parse was changed")
	}
	if read(t, filepath.Join(dir, "specs/claims.feature")) != formattedFeature {
		t.Error("the other features were not formatted")
	}
	if !strings.Contains(out, "formatted specs/claims.feature") {
		t.Errorf("output:\n%s", out)
	}
}

func TestFmtPathNotFound(t *testing.T) {
	fmtProject(t)
	_, stderr, code := run(t, "fmt", "nowhere")
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "AXX-E0201") || !strings.Contains(stderr, "the paths given to `axx fmt`") {
		t.Errorf("exit %d:\n%s", code, stderr)
	}
}

// axx fmt only lays out feature files: it never prepares the project's packs,
// so it neither waits for their build nor warns that they could not be
// prepared, and no pack's hook runs with it.
func TestFmtRunsWithoutTheProjectsPacks(t *testing.T) {
	t.Setenv("AXX_SOURCE_DIR", "")
	agentProject(t, map[string]string{
		"axx.yaml":                "version: 1\n",
		"axx-packs.yaml":          "packs:\n  - ./hook-probe\n  - ./not-compiled-in\n",
		"features/claims.feature": unformattedFeature,
	})
	takeHookEvents()
	out, errOut, code := run(t, "fmt")
	if code != int(exitcode.OK) || strings.Contains(errOut, "packs") {
		t.Fatalf("exit %d, out %q, stderr:\n%s", code, out, errOut)
	}
	if got := read(t, "features/claims.feature"); got != formattedFeature {
		t.Errorf("not formatted:\n%s", got)
	}
	if lines, _ := takeHookEvents(); len(lines) != 0 {
		t.Errorf("hooks ran: %q", lines)
	}
}

const formattedFeatureIndentFour = "Feature: Claims\n\n    Scenario: A claim\n        Given a thing\n            | a   | bb |\n            | ccc | d  |\n"

// axx.yaml's fmt.indent is how far in each level sits, with paths or
// without; an indent axx does not offer stops before any file changes.
func TestFmtLaysOutAsAxxYamlSays(t *testing.T) {
	dir := fmtProject(t)
	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\nrun:\n  paths: [specs]\nfmt:\n  indent: 4\n"})
	if out, stderr, code := run(t, "fmt"); code != int(exitcode.OK) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	if got := read(t, filepath.Join(dir, "specs/claims.feature")); got != formattedFeatureIndentFour {
		t.Errorf("without paths:\n%s", got)
	}
	if out, stderr, code := run(t, "fmt", "other"); code != int(exitcode.OK) {
		t.Fatalf("exit %d\n%s%s", code, out, stderr)
	}
	if got := read(t, filepath.Join(dir, "other/claims.feature")); got != formattedFeatureIndentFour {
		t.Errorf("with a path:\n%s", got)
	}

	writeFiles(t, dir, map[string]string{"axx.yaml": "version: 1\nfmt:\n  indent: 3\n", "features/x.feature": unformattedFeature})
	out, stderr, code := run(t, "fmt")
	if code == int(exitcode.OK) || !strings.Contains(out+stderr, "indent") {
		t.Errorf("indent 3: exit %d\n%s%s", code, out, stderr)
	}
	if got := read(t, filepath.Join(dir, "features/x.feature")); got != unformattedFeature {
		t.Errorf("indent 3 changed the file:\n%s", got)
	}
}
