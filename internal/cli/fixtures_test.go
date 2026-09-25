package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/fixtures"
)

// fixturesProject lays out an axx project whose fixtures live under
// resources/ (fixtures.baseDir), seeded with the json test corpus.
func fixturesProject(t *testing.T) (dir, config string) {
	t.Helper()
	dir = t.TempDir()
	src := filepath.Join("..", "fixtures", "testdata", "corpus", "json-corpus")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		target := filepath.Join(dir, "resources", rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	config = filepath.Join(dir, "axx.yaml")
	writeFile(t, config, `version: 1
fixtures:
  baseDir: resources
  conformance:
    - name: ingested telemetry
      filePatterns: ["ingest/*.json"]
      schemaType: json
      schemaRef: schemas/telemetry.schema.json
`)
	return dir, config
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func envelope(t *testing.T, out string) Envelope {
	t.Helper()
	var env Envelope
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	return env
}

func TestFixturesGenerateThenCheck(t *testing.T) {
	dir, cfg := fixturesProject(t)
	out, stderr, code := run(t, "fixtures", "generate", "--config", cfg)
	if code != 0 {
		t.Fatalf("generate exit %d: %s", code, stderr)
	}
	if !strings.Contains(out, "wrote 7 files, 0 unchanged") {
		t.Errorf("summary: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", fixtures.ManifestFile)); err != nil {
		t.Error("the manifest lives in fixtures.baseDir")
	}
	out, _, code = run(t, "fixtures", "check", "--config", cfg)
	if code != 0 || !strings.Contains(out, "checks passed") {
		t.Errorf("check exit %d: %s", code, out)
	}
	out, _, code = run(t, "fixtures", "generate", "--config", cfg, "--json")
	env := envelope(t, out)
	data, _ := env.Data.(map[string]any)
	if code != 0 || !env.OK || env.Command != "axx fixtures generate" || len(data["unchanged"].([]any)) != 7 {
		t.Errorf("json: %s", out)
	}
}

func TestFixturesCheckFailuresExitOne(t *testing.T) {
	dir, cfg := fixturesProject(t)
	run(t, "fixtures", "generate", "--config", cfg)
	gold := filepath.Join(dir, "resources", "limits", "gold-tier.json")
	b, _ := os.ReadFile(gold)
	writeFile(t, gold, strings.Replace(string(b), "USD", "EUR", 1))
	out, _, code := run(t, "fixtures", "check", "--config", cfg)
	if code != int(exitcode.Failed) || !strings.Contains(out, "FIXTURE DRIFT") || !strings.Contains(out, "1 of") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	out, _, code = run(t, "fixtures", "check", "--config", cfg, "--json")
	env := envelope(t, out)
	data := env.Data.(map[string]any)
	failures := data["failures"].([]any)
	if code != 1 || env.OK || len(failures) != 1 || failures[0].(map[string]any)["code"] != fixtures.CodeCheck {
		t.Errorf("json: %s", out)
	}
}

func TestFixturesHandEditRefusal(t *testing.T) {
	dir, cfg := fixturesProject(t)
	run(t, "fixtures", "generate", "--config", cfg)
	gold := filepath.Join(dir, "resources", "limits", "gold-tier.json")
	b, _ := os.ReadFile(gold)
	writeFile(t, gold, strings.Replace(string(b), "USD", "EUR", 1))
	_, stderr, code := run(t, "fixtures", "generate", "--config", cfg)
	if code != int(exitcode.Failed) || !strings.Contains(stderr, fixtures.CodeHandEdit) || !strings.Contains(stderr, "gold-tier.json") {
		t.Errorf("exit %d: %s", code, stderr)
	}
}

func TestFixturesAdoptEndToEnd(t *testing.T) {
	dir, cfg := fixturesProject(t)
	out, stderr, code := run(t, "fixtures", "adopt", "--config", cfg, "--family", "json",
		"--schema", "schemas/telemetry.schema.json", "--files", "ingest/*.json", "--factory", "adopted")
	if code != 0 || !strings.Contains(out, "deep-equal") {
		t.Fatalf("exit %d: %s%s", code, out, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", "ingest", "adopted.factory.yaml")); err != nil {
		t.Error(err)
	}
}

func TestFixturesUsageAndConfigErrors(t *testing.T) {
	dir, cfg := fixturesProject(t)
	tests := []struct {
		args []string
		code exitcode.Code
		want string
	}{
		{[]string{"fixtures", "adopt", "--config", cfg, "--schema", "x"}, exitcode.Usage, "AXX-E0909"},
		{[]string{"fixtures", "adopt", "--config", cfg, "--files", "x"}, exitcode.Usage, "missing --schema"},
		{[]string{"fixtures", "adopt", "--config", cfg, "--into", "a", "--schema", "b", "--files", "c"}, exitcode.Usage, "drop --schema"},
		{[]string{"fixtures", "generate", "--config", cfg, "extra"}, exitcode.Usage, "AXX-E0001"},
		{[]string{"schema", "--kind", "nope"}, exitcode.Usage, "AXX-E0010"},
	}
	for _, tc := range tests {
		_, stderr, code := run(t, tc.args...)
		if code != int(tc.code) || !strings.Contains(stderr, tc.want) {
			t.Errorf("%v: exit %d: %s", tc.args, code, stderr)
		}
	}
	factory := filepath.Join(dir, "resources", "devices", "device-events.factory.yaml")
	b, _ := os.ReadFile(factory)
	writeFile(t, factory, strings.Replace(string(b), "family: json", "family: nope", 1))
	_, stderr, code := run(t, "fixtures", "generate", "--config", cfg)
	if code != int(exitcode.Usage) || !strings.Contains(stderr, "unsupported family 'nope'") || !strings.Contains(stderr, fixtures.CodeSpec) {
		t.Errorf("exit %d: %s", code, stderr)
	}
}

func TestFixturesCleanAndUntrackDryRun(t *testing.T) {
	dir, cfg := fixturesProject(t)
	writeFile(t, cfg, "version: 1\nfixtures:\n  baseDir: resources\n  output: { ignored: true }\n")
	run(t, "fixtures", "generate", "--config", cfg)
	if _, err := os.Stat(filepath.Join(dir, "resources", "limits", ".gitignore")); err != nil {
		t.Fatal("ignored outputs get a .gitignore")
	}
	out, _, code := run(t, "fixtures", "clean", "--config", cfg, "--dry-run", "--json")
	env := envelope(t, out)
	if code != 0 || len(env.Data.(map[string]any)["deleted"].([]any)) != 6 {
		t.Errorf("clean dry run: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", "limits", "gold-tier.json")); err != nil {
		t.Error("a dry run deletes nothing")
	}
	out, _, code = run(t, "fixtures", "clean", "--config", cfg)
	if code != 0 || !strings.Contains(out, "deleted 6 ignored outputs") {
		t.Errorf("clean: %s", out)
	}
}

func TestSchemaKinds(t *testing.T) {
	for _, kind := range fixtures.SchemaKinds {
		out, _, code := run(t, "schema", "--kind", kind)
		if code != 0 || !strings.Contains(out, fixtures.SchemaID(kind)) {
			t.Errorf("%s: exit %d", kind, code)
		}
	}
	out, _, _ := run(t, "schema")
	if !strings.Contains(out, "axx.schema.json") {
		t.Error("the default kind is axx.yaml")
	}
}
