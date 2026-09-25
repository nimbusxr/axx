package fixtures

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// exampleConfig is examples/parcels/acceptance/axx.yaml's fixtures
// section.
func exampleConfig(acceptance string) Config {
	return Config{
		BaseDir: acceptance,
		Sources: []string{"../infra/wiremock"},
		Conformance: []ConformanceRule{{
			Name: "seeds match the database schema", FilePatterns: []string{"seeds/*.yaml"},
			SchemaType: "dataset", SchemaRef: "../infra/postgres/init/01-schema.sql",
		}},
		LintEmit:      true,
		OutputIgnored: true,
	}.withDefaults()
}

var update = flag.Bool("update", false, "regenerate the committed example sources (manifest, .gitignores, lint rules)")

// TestExampleRoundTrip regenerates the parcels example from scratch
// and compares every produced file with the committed manifest's sha256.
func TestExampleRoundTrip(t *testing.T) {
	withoutAvroOracle(t)
	if *update {
		acceptance := filepath.Join(repoRoot(), "examples", "parcels", "acceptance")
		g, err := NewGenerator(exampleConfig(acceptance), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := g.Generate(); err != nil {
			t.Fatal(err)
		}
		if _, err := g.Clean(false); err != nil { // ignored outputs are not committed
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	copyTree(t, filepath.Join(repoRoot(), "examples", "parcels"), dir)
	acceptance := filepath.Join(dir, "acceptance")
	committed, err := LoadManifest(acceptance)
	if err != nil {
		t.Fatal(err)
	}
	if committed.Len() == 0 {
		t.Fatal("the example has no manifest")
	}
	// Delete every generated output (ignored outputs are not in the checkout
	// anyway); keep the manifest so hand-edit checks apply.
	for _, e := range committed.Entries() {
		if err := os.Remove(filepath.Join(acceptance, filepath.FromSlash(e.Path))); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	g, err := NewGenerator(exampleConfig(acceptance), Options{})
	if err != nil {
		t.Fatal(err)
	}
	res, err := g.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Written) != committed.Len() {
		t.Errorf("wrote %d files, the manifest lists %d: %v", len(res.Written), committed.Len(), res.Written)
	}
	for _, e := range committed.Entries() {
		data, err := os.ReadFile(filepath.Join(acceptance, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Errorf("%s: %v", e.Path, err)
			continue
		}
		if got := SHA256(data); got != e.SHA256 {
			t.Errorf("%s: sha256 %s, manifest has %s\n%s", e.Path, got, e.SHA256, data)
		}
	}
	regenerated, err := os.ReadFile(filepath.Join(acceptance, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	original, _ := os.ReadFile(filepath.Join(repoRoot(), "examples", "parcels", "acceptance", ManifestFile))
	if string(regenerated) != string(original) {
		t.Errorf("manifest differs:\n--- committed\n%s\n+++ regenerated\n%s", original, regenerated)
	}

	checker, err := NewChecker(exampleConfig(acceptance), Options{})
	if err != nil {
		t.Fatal(err)
	}
	total, failures := checker.Run()
	if len(failures) > 0 {
		t.Errorf("check failed: %+v", failures)
	}
	if total == 0 {
		t.Error("no checks ran")
	}
}
