package fixtures

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Additional sources roots: fixture sources outside the resource root (a
// compose-mounted infra directory), walked exactly like the root itself so
// fixtures sit next to generated files that must stay outside.

func sourcesWorkspace(t *testing.T) (*workspace, string) {
	t.Helper()
	repo := t.TempDir()
	resources := filepath.Join(repo, "acceptance", "src", "test", "resources")
	if err := os.MkdirAll(filepath.Join(resources, "schemas"), 0o755); err != nil {
		t.Fatal(err)
	}
	w := &workspace{t: t, dir: resources, cfg: DefaultConfig(resources)}
	w.cfg.Sources = []string{"../../../../infra/wiremock"}
	w.write("../../../../infra/wiremock/pong/pong.schema.json", `{"$schema": "https://json-schema.org/draft/2020-12/schema", "title": "Pong",
 "type": "object", "required": ["status"],
 "properties": {"status": {"type": "string"}, "message": {"type": "string"}},
 "additionalProperties": false}
`)
	w.write("../../../../infra/wiremock/pong/__files/pong-bodies.factory.yaml", "factory:\n  family: json\n  schema: ../pong.schema.json\n")
	w.write("../../../../infra/wiremock/pong/__files/pong-bodies.prototype.yaml", "data:\n  status: ok\n")
	w.write("../../../../infra/wiremock/pong/__files/ping-pong.fixture.yaml", "data:\n  message: table tennis\n")
	return w, repo
}

func TestSourcesInAnOutsideRootGenerateColocatedOutputs(t *testing.T) {
	w, repo := sourcesWorkspace(t)
	body := "../../../../infra/wiremock/pong/__files/ping-pong.json"
	files := w.expand()
	if files[body] == nil {
		t.Fatalf("files: %v", keysOf(files))
	}
	w.generate()
	out := filepath.Join(repo, "infra/wiremock/pong/__files/ping-pong.json")
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(data), `"status": "ok"`, "table tennis")
	for p, b := range w.expand() {
		if !bytes.Equal(b, files[p]) {
			t.Errorf("%s not byte-stable", p)
		}
	}
	if f := w.check(); len(f) != 0 {
		t.Fatalf("check: %v", f)
	}
	if err := os.WriteFile(out, bytes.Replace(data, []byte("ok"), []byte("ko"), 1), 0o644); err != nil {
		t.Fatal(err)
	}
	f := w.check()
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "FIXTURE DRIFT")
}

func TestSourcesIgnoredOutputsMaintainGitignoresOutside(t *testing.T) {
	w, repo := sourcesWorkspace(t)
	w.replace("../../../../infra/wiremock/pong/__files/pong-bodies.factory.yaml", "family: json", "family: json\n  output: { ignored: true }")
	w.generate()
	data, err := os.ReadFile(filepath.Join(repo, "infra/wiremock/pong/__files/.gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, string(data), "/ping-pong.json")
}

func TestSourcesConformanceRulesReachIntoOutsideRoots(t *testing.T) {
	w, _ := sourcesWorkspace(t)
	w.write("../../../../infra/wiremock/pong/__files/legacy-bad.json", `{"status": 12345}`+"\n")
	w.conformance(ConformanceRule{
		Name: "pong bodies conform", FilePatterns: []string{"../../../../infra/wiremock/pong/__files/*.json"},
		SchemaType: "json", SchemaRef: "../../../../infra/wiremock/pong/pong.schema.json",
	})
	w.generate()
	f := filtered(w.check(), "conformance")
	if len(f) != 1 {
		t.Fatalf("failures: %v", f)
	}
	mustContain(t, f[0], "legacy-bad.json")
}

func TestSourcesAdoptionWritesTheFactoryNextToOutsideFiles(t *testing.T) {
	w, repo := sourcesWorkspace(t)
	w.write("../../../../infra/wiremock/legacy/greet.json", `{"status": "ok", "message": "hello"}`+"\n")
	w.write("../../../../infra/wiremock/legacy/farewell.json", `{"status": "ok", "message": "goodbye"}`+"\n")
	res, err := w.adopter().Adopt("json", "../../../../infra/wiremock/pong/pong.schema.json", "../../../../infra/wiremock/legacy/*.json", "legacy-bodies", false)
	if err != nil {
		t.Fatal(err)
	}
	mustContain(t, joinLines(res.Report), "2/2 deep-equal")
	for _, p := range []string{"infra/wiremock/legacy/legacy-bodies.factory.yaml", "infra/wiremock/legacy/greet.fixture.yaml"} {
		if _, err := os.Stat(filepath.Join(repo, p)); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestSourcesMissingAndOverlappingRootsAreLoudErrors(t *testing.T) {
	for sources, want := range map[string]string{
		"../../../../nowhere": "does not exist",
		"schemas":             "inside the resource root",
		"..":                  "contains the resource root",
	} {
		w, _ := sourcesWorkspace(t)
		w.cfg.Sources = []string{sources}
		mustErrContain(t, w.expandErr(), want)
	}
}
