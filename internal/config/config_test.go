package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

const sample = `version: 1
run:
  paths: [features]
  workers: auto
  exclusive: ["@isolated"]
  reporters: [pretty, {junit: build/axx/junit.xml}]
properties:
  local.host: localhost
services:
  zeta:
    command: ./gradlew bootRun
    ready:
      http: {url: "http://${sys:local.host}:8080/actuator/health"}
      timeout: 90s
  alpha:
    command: [docker, compose, up]
    dependsOn: [zeta]
    cleanup: docker compose down -v
profiles:
  ci:
    properties:
      local.host: docker
    run:
      workers: 8
`

func TestLoadSample(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", sample)
	cfg, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Services) != 2 || cfg.Services[0].Name != "zeta" || cfg.Services[1].Name != "alpha" {
		t.Fatalf("services must keep declaration order: %+v", cfg.Services)
	}
	if got := cfg.Services[0].Ready.HTTP.URL[0]; got != "http://localhost:8080/actuator/health" {
		t.Errorf("interpolated url = %q", got)
	}
	if cfg.Services[0].Ready.Timeout.D().Seconds() != 90 {
		t.Errorf("timeout = %v", cfg.Services[0].Ready.Timeout.D())
	}
	if cfg.Services[1].Command.Argv[0] != "docker" || cfg.Services[0].Command.Line != "./gradlew bootRun" {
		t.Errorf("commands: %+v %+v", cfg.Services[0].Command, cfg.Services[1].Command)
	}
	if cfg.Run.Workers != 0 || len(cfg.Run.Reporters) != 2 || cfg.Run.Reporters[1].Path != "build/axx/junit.xml" {
		t.Errorf("run: %+v", cfg.Run)
	}
	if cfg.Dir != dir {
		t.Errorf("dir = %q", cfg.Dir)
	}
}

func TestProfileAndOverrides(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", sample)
	cfg, err := Load(LoadOptions{WorkDir: dir, Profile: "ci", LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Run.Workers != 8 {
		t.Errorf("profile workers = %d", cfg.Run.Workers)
	}
	if got := cfg.Services[0].Ready.HTTP.URL[0]; !strings.Contains(got, "//docker:") {
		t.Errorf("profile property not applied: %q", got)
	}
	// -D wins over profile
	cfg, err = Load(LoadOptions{WorkDir: dir, Profile: "ci", Properties: map[string]string{"local.host": "cli"}, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services[0].Ready.HTTP.URL[0]; !strings.Contains(got, "//cli:") {
		t.Errorf("-D not applied: %q", got)
	}
	// AXX_PROFILE selects the profile; axx.local.yaml overlays last
	write(t, dir, "axx.local.yaml", "run:\n  workers: 3\n")
	cfg, err = Load(LoadOptions{WorkDir: dir, LookupEnv: env(map[string]string{"AXX_PROFILE": "ci"})})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Run.Workers != 3 {
		t.Errorf("local overlay workers = %d", cfg.Run.Workers)
	}
}

// A property may use another, from the file, a profile or -D: steps read
// it expanded, as the rest of axx.yaml has it.
func TestPropertiesUseProperties(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", `version: 1
properties:
  python: python3
  desk.app: ""
profiles:
  qt:
    properties:
      desk.app: ${sys:python}
`)
	cfg, err := Load(LoadOptions{WorkDir: dir, Profile: "qt", LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Properties["desk.app"]; got != "python3" {
		t.Errorf("desk.app = %q, want python3", got)
	}
	cfg, err = Load(LoadOptions{WorkDir: dir, Profile: "qt", Properties: map[string]string{"python": "/opt/qt/bin/python"}, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Properties["desk.app"]; got != "/opt/qt/bin/python" {
		t.Errorf("with -D python: desk.app = %q", got)
	}
}

// Profiles combine: watch,ios applies watch, then ios over it.
func TestProfilesCombine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", `version: 1
run:
  workers: 4
profiles:
  watch:
    run: {watch: true, slowdown: 300ms, workers: 1}
  ios:
    run: {tags: "@ios", slowdown: 1s}
`)
	cfg, err := Load(LoadOptions{WorkDir: dir, Profile: "watch, ios", LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Run.Watch || cfg.Run.Workers != 1 || cfg.Run.Tags != "@ios" || cfg.Run.Slowdown.D() != time.Second {
		t.Errorf("combined profiles: %+v", cfg.Run)
	}
	_, err = Load(LoadOptions{WorkDir: dir, Profile: "watch,android", LookupEnv: env(nil)})
	if err == nil || !strings.Contains(err.Error(), `profile "android" not found`) {
		t.Errorf("an unknown second profile: %v", err)
	}
}

func TestMissingConfigIsFine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	cfg, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err != nil || cfg.File != "" || len(cfg.Services) != 0 {
		t.Fatalf("expected defaults, got %+v, %v", cfg, err)
	}
}

func TestSearchUpward(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".git/HEAD", "x")
	write(t, dir, "axx.yaml", "run: {paths: [acceptance]}\n")
	sub := filepath.Join(dir, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(LoadOptions{WorkDir: sub, LookupEnv: env(nil)})
	if err != nil || cfg.Dir != dir || cfg.Run.Paths[0] != "acceptance" {
		t.Fatalf("upward search failed: %+v %v", cfg, err)
	}
}

func TestSchemaErrorsHaveLocations(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", "run:\n  workers: many\nservices:\n  api:\n    dir: x\n")
	_, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeInvalid || ae.Exit != exitcode.Usage {
		t.Fatalf("want invalid config error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"run.workers: value must be 'auto', or an integer", "services.api", "command", "axx.yaml:2:12", "axx.yaml:5:8"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should mention %q:\n%s", want, msg)
		}
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", "rnu:\n  paths: [x]\n")
	_, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err == nil || !strings.Contains(err.Error(), "rnu") {
		t.Fatalf("unknown top-level key must be rejected, got %v", err)
	}
}

func TestSyntaxError(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", "run:\n  paths: [a\n")
	_, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeSyntax {
		t.Fatalf("want syntax error, got %v", err)
	}
}

func TestSchemaUpToDate(t *testing.T) {
	want, err := func() ([]byte, error) {
		wd, _ := os.Getwd()
		defer os.Chdir(wd) //nolint:errcheck
		if err := os.Chdir("../.."); err != nil {
			return nil, err
		}
		return Generate(true)
	}()
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(SchemaJSON) {
		t.Fatal("internal/config/axx.schema.json is stale; run `go generate ./internal/config`")
	}
}

func TestLintSection(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", `version: 1
lint:
  include: extra.yaml
  config: {baseDir: data, mode: warn, maxFileSize: 1000}
  rules:
    - name: ids
      filePatterns: "seeds/*.yaml"
      regex: 'id: "([^"]+)"'
      ignoreValues: [shared, 7, true, 1.5]
`)
	cfg, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.Lint
	if l == nil || l.Include[0] != "extra.yaml" || l.Config.BaseDir != "data" || l.Config.MaxFileSize != 1000 || l.Rules[0].FilePatterns[0] != "seeds/*.yaml" {
		t.Fatalf("lint: %+v", l)
	}
	if got := strings.Join(l.Rules[0].IgnoreValues, ","); got != "shared,7,true,1.5" {
		t.Errorf("ignoreValues = %s", got)
	}
	if loc, ok := cfg.Position("lint", "rules", "0", "regex"); !ok || loc.Line != 8 {
		t.Errorf("position = %+v %v", loc, ok)
	}
	if line, _, ok := YAMLPosition([]byte("rules:\n  - name: x\n    regex: y\n"), "rules", "0", "regex"); !ok || line != 3 {
		t.Errorf("YAMLPosition line = %d", line)
	}

	write(t, dir, "axx.yaml", "version: 1\nlint:\n  rules:\n    - {name: ids, filePatterns: [x], regex: '(x)', ignoreValues: [{a: 1}]}\n")
	_, err = Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeInvalid || !strings.Contains(err.Error(), "axx.yaml:4:") {
		t.Errorf("an object in ignoreValues is a schema error: %v", err)
	}
}

func TestLoadLintFile(t *testing.T) {
	dir := t.TempDir()
	good := write(t, dir, "good.yaml", "include: [more.yaml]\nrules:\n  - {name: r, filePatterns: [x], type: jsonpath, jsonPath: a.b}\n")
	l, err := LoadLintFile(good)
	if err != nil || len(l.Rules) != 1 || l.Include[0] != "more.yaml" || l.Config != nil {
		t.Fatalf("LoadLintFile: %+v %v", l, err)
	}
	for name, content := range map[string]string{
		"syntax.yaml": "rules: [\n",
		"schema.yaml": "rules:\n  - name: r\n    filePatterns: [x]\n    validation: sometimes\n",
		"list.yaml":   "- a\n",
	} {
		_, err := LoadLintFile(write(t, dir, name, content))
		var ae *axxerr.Error
		if !errors.As(err, &ae) || ae.Code != CodeLintInclude || ae.Hint == "" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if l, err := LoadLintFile(write(t, dir, "empty.yaml", "# nothing\n")); err != nil || len(l.Rules) != 0 {
		t.Errorf("an empty file has no rules: %+v %v", l, err)
	}
}
