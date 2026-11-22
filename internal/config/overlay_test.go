package config

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The overlays' schema is made from axx.yaml's, by go generate.
func TestOverlaySchemaUpToDate(t *testing.T) {
	want, err := GenerateOverlay(SchemaJSON)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(want, OverlaySchemaJSON) {
		t.Fatal("schemas/v0/axx-overlay.schema.json is stale; run `go generate ./internal/config`")
	}
}

// overlayIssues are what the overlays' schema refuses in a file, each as "where: what".
func overlayIssues(t *testing.T, file string) []string {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(OverlaySchemaJSON))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(OverlaySchemaID, doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile(OverlaySchemaID)
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := yaml.Unmarshal([]byte(file), &v); err != nil {
		t.Fatal(err)
	}
	err = sch.Validate(v)
	if err == nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(err.Error()), "\n")
}

// A profile's file says only what differs from axx.yaml, which axx lays it over: a service it
// switches off, or whose readiness check it gives more time, has its command in axx.yaml. The
// schema an editor holds the file to asks for none of what axx.yaml's requires of a mapping.
func TestAnOverlaySaysOnlyWhatDiffers(t *testing.T) {
	for name, file := range map[string]string{
		"a service switched off": `
services:
  kafka:
    enabled: false
`,
		"a readiness check given more time, a debugger's host, a runtime's mount": `
services:
  orders:
    ready:
      timeout: 2m
    debug:
      debugger:
        host: 127.0.0.1
    coverage:
      mount: /axx-coverage
`,
		"a setting of the run": `
run:
  tags: "@smoke"
`,
		"nothing": `{}`,
	} {
		if issues := overlayIssues(t, file); len(issues) > 0 {
			t.Errorf("%s: the overlays' schema refuses it:\n  %s", name, strings.Join(issues, "\n  "))
		}
	}
}

// It is still axx.yaml's schema in all else: a key axx.yaml has not, or a value of the wrong
// kind, is refused. And a list in an overlay replaces the list under it, so its entries are
// whole: what a list's entry requires is still required.
func TestAnOverlayIsHeldToAxxYAMLsKeysAndKinds(t *testing.T) {
	for name, tc := range map[string]struct{ file, want string }{
		"a key axx.yaml has not": {`
services:
  kafka:
    enabld: false
`, "enabld"},
		"a value of the wrong kind": {`
services:
  kafka:
    enabled: "no"
`, "enabled"},
		"a list's entry without what it requires": {`
hooks:
  commands:
    - before: run
`, "command"},
	} {
		issues := overlayIssues(t, tc.file)
		if len(issues) == 0 || !strings.Contains(strings.Join(issues, "\n"), tc.want) {
			t.Errorf("%s: the overlays' schema says %q, want it to refuse %s", name, issues, tc.want)
		}
	}
	// The same entry, whole.
	if issues := overlayIssues(t, "hooks:\n  commands:\n    - before: run\n      command: make seed\n"); len(issues) > 0 {
		t.Errorf("a whole entry of a list is refused: %s", issues)
	}
}

// axx itself goes by the result: a profile's file that switches a service off, with no command
// of its own, loads, and the service is off.
func TestAProfilesFileSwitchesAServiceOff(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "axx.yaml", "services:\n  kafka:\n    command: docker compose up kafka\n  orders:\n    command: ./orders\n")
	write(t, dir, "axx.apollo.yaml", "services:\n  kafka:\n    enabled: false\n")
	cfg, err := Load(LoadOptions{WorkDir: dir, Profile: "apollo"})
	if err != nil {
		t.Fatal(err)
	}
	on := map[string]bool{}
	for _, svc := range cfg.Services {
		on[svc.Name] = svc.IsEnabled()
		if svc.Name == "kafka" && svc.Command.Line != "docker compose up kafka" {
			t.Errorf("kafka's command under the profile: %+v", svc.Command)
		}
	}
	if on["kafka"] || !on["orders"] {
		t.Errorf("under the profile kafka is on = %v, and orders, which it does not name, on = %v", on["kafka"], on["orders"])
	}
}
