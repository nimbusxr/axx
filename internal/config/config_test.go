package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
apps:
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
	if len(cfg.Apps) != 2 || cfg.Apps[0].Name != "zeta" || cfg.Apps[1].Name != "alpha" {
		t.Fatalf("apps must keep declaration order: %+v", cfg.Apps)
	}
	if got := cfg.Apps[0].Ready.HTTP.URL[0]; got != "http://localhost:8080/actuator/health" {
		t.Errorf("interpolated url = %q", got)
	}
	if cfg.Apps[0].Ready.Timeout.D().Seconds() != 90 {
		t.Errorf("timeout = %v", cfg.Apps[0].Ready.Timeout.D())
	}
	if cfg.Apps[1].Command.Argv[0] != "docker" || cfg.Apps[0].Command.Line != "./gradlew bootRun" {
		t.Errorf("commands: %+v %+v", cfg.Apps[0].Command, cfg.Apps[1].Command)
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
	if got := cfg.Apps[0].Ready.HTTP.URL[0]; !strings.Contains(got, "//docker:") {
		t.Errorf("profile property not applied: %q", got)
	}
	// -D wins over profile
	cfg, err = Load(LoadOptions{WorkDir: dir, Profile: "ci", Properties: map[string]string{"local.host": "cli"}, LookupEnv: env(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Apps[0].Ready.HTTP.URL[0]; !strings.Contains(got, "//cli:") {
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

func TestMissingConfigIsFine(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".git/HEAD", "ref: refs/heads/main\n")
	cfg, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	if err != nil || cfg.File != "" || len(cfg.Apps) != 0 {
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
	write(t, dir, "axx.yaml", "run:\n  workers: many\napps:\n  api:\n    dir: x\n")
	_, err := Load(LoadOptions{WorkDir: dir, LookupEnv: env(nil)})
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeInvalid || ae.Exit != exitcode.Usage {
		t.Fatalf("want invalid config error, got %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"run.workers", "apps.api", "command", "axx.yaml:2:12", "axx.yaml:5:8"} {
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
