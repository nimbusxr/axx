package lint

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

// goldenReport is a small suite exercising every kind of finding.
func goldenReport(t *testing.T) (*Report, string) {
	t.Helper()
	p := newProject(t).
		write("seeds/missions.yaml", "space.missions:\n  - id: \"mission-1\"\n    status: \"planned\"\n  - id: \"mission-2\"\n    status: \"launched\"\n").
		write("seeds/mock-data.yaml", "space.missions:\n  - id: \"mission-1\"\n    status: \"planned\"\n").
		write("seeds/legacy.yaml", "space.missions:\n  - id: \"legacy-1\"\n  - id: \"legacy-1\"\n").
		write("kafka/mission-created.json", "{\n  \"event_type\": \"created\",\n  \"mission_id\": \"m-1\"\n}\n").
		write("kafka/mission-launched.json", "{\"mission_id\": \"m-1\", \"note\": \"a, b: c%\"}\n").
		write("kafka/broken.json", "{\n  \"mission_id\": 'm-2'\n}\n").
		write("big/huge.yaml", strings.Repeat("# padding\n", 20))
	rep := p.run(`config:
  baseDir: .
  maxFileSize: 150
rules:
  - name: "Database seed IDs"
    description: "Seed row IDs must be unique across all seed files"
    filePatterns: ["seeds/*.yaml", "big/*.yaml"]
    excludePatterns: ["seeds/legacy.yaml"]
    regex: '^\s+-?\s*id:\s*"([^"]+)"'
    validation: cross-file-unique
  - name: "Legacy IDs"
    filePatterns: ["seeds/legacy.yaml"]
    regex: 'id: "([^"]+)"'
    mode: warn
  - name: "Status values"
    filePatterns: ["seeds/*.yaml"]
    regex: 'status: "([^"]+)"'
  - name: "Event types"
    filePatterns: ["kafka/mission-*.json"]
    type: jsonpath
    jsonPath: $.event_type
  - name: "mission_id uniqueness"
    filePatterns: ["kafka/*.json"]
    type: jsonpath
    jsonPath: mission_id
    validation: cross-file-unique
`)
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	src := "Feature: f\n  Scenario: s\n    Then the 2nd selection has 1 row\n"
	p.write("features/f.feature", src)
	_, pickles, err := feature.ParseSource("features/f.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	for _, pk := range pickles {
		pk.Doc.Path = filepath.Join(p.dir, "features/f.feature")
	}
	rep.Add(CheckFeatures(e.Registry, pickles, p.dir))
	return rep, p.dir
}

func golden(t *testing.T, name string, got []byte, dir string) {
	t.Helper()
	if slashed := filepath.ToSlash(dir); !strings.HasPrefix(slashed, "/") {
		// Windows: the SARIF root is file:///C:/..., the directory with slashes after a slash.
		got = bytes.ReplaceAll(got, []byte("/"+slashed), []byte("/ROOT"))
	}
	got = bytes.ReplaceAll(got, []byte(dir), []byte("/ROOT"))
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/lint -run TestFormats -update)", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file (run with -update to accept):\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func TestFormats(t *testing.T) {
	rep, dir := goldenReport(t)
	if rep.OK() || rep.Summary.Errors != 4 || rep.Summary.Warnings != 3 {
		t.Fatalf("summary: %+v", rep.Summary)
	}
	render := map[string]func(*bytes.Buffer) error{
		"human.txt":         func(b *bytes.Buffer) error { return WriteHuman(b, rep, HumanOptions{}) },
		"human-compact.txt": func(b *bytes.Buffer) error { return WriteHuman(b, rep, HumanOptions{Compact: true}) },
		"junit.xml":         func(b *bytes.Buffer) error { return WriteJUnit(b, rep) },
		"sarif.json":        func(b *bytes.Buffer) error { return WriteSARIF(b, rep, SARIFOptions{Version: "0.0.0-test", Root: dir}) },
		"github.txt":        func(b *bytes.Buffer) error { return WriteGitHub(b, rep, dir) },
		"report.json": func(b *bytes.Buffer) error {
			enc := json.NewEncoder(b)
			enc.SetIndent("", "  ")
			enc.SetEscapeHTML(false)
			return enc.Encode(rep)
		},
	}
	for name, fn := range render {
		t.Run(name, func(t *testing.T) {
			var b bytes.Buffer
			if err := fn(&b); err != nil {
				t.Fatal(err)
			}
			golden(t, name, b.Bytes(), dir)
		})
	}

	// The machine formats are well formed.
	var b bytes.Buffer
	_ = WriteJUnit(&b, rep)
	var suites junitSuites
	if err := xml.Unmarshal(b.Bytes(), &suites); err != nil || suites.Tests != 6 || suites.Failures != 3 {
		t.Errorf("junit: %v %+v", err, suites)
	}
	b.Reset()
	_ = WriteSARIF(&b, rep, SARIFOptions{Root: dir})
	var log sarifLog
	if err := json.Unmarshal(b.Bytes(), &log); err != nil || log.Version != "2.1.0" || len(log.Runs[0].Tool.Driver.Rules) != 6 {
		t.Fatalf("sarif: %v", err)
	}
	for _, r := range log.Runs[0].Results {
		if r.RuleID != log.Runs[0].Tool.Driver.Rules[r.RuleIndex].ID {
			t.Errorf("ruleIndex %d does not point at %s", r.RuleIndex, r.RuleID)
		}
	}
}

func TestGitHubEscaping(t *testing.T) {
	if got := ghProp("a,b:c%\nd"); got != "a%2Cb%3Ac%25%0Ad" {
		t.Errorf("ghProp = %q", got)
	}
	if got := ghData("a,b:c%\r\n"); got != "a,b:c%25%0D%0A" {
		t.Errorf("ghData = %q", got)
	}
}

func TestRepoRoot(t *testing.T) {
	p := newProject(t).write("repo/.git/HEAD", "ref").write("repo/a/b/x", "")
	if got := RepoRoot(filepath.Join(p.dir, "repo/a/b"), func(string) string { return "" }); got != filepath.Join(p.dir, "repo") {
		t.Errorf("RepoRoot = %s", got)
	}
	if got := RepoRoot(p.dir, func(k string) string { return map[string]string{"GITHUB_WORKSPACE": "/ws"}[k] }); got != "/ws" {
		t.Errorf("RepoRoot with GITHUB_WORKSPACE = %s", got)
	}
}
