package fixtures

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExpectedOutputs expands every test corpus (and the dataset format
// variants) and compares each produced file, byte for byte, with the expected
// output recorded in testdata/expected.
func TestExpectedOutputs(t *testing.T) {
	cases := []struct {
		name  string
		seed  []string
		edit  [3]string
		avro  bool
		files int
	}{
		{name: "avro", seed: []string{"kafka", "schemas"}, avro: true, files: 4},
		{name: "json", seed: []string{"json-corpus"}, files: 7},
		{name: "openapi", seed: []string{"openapi-corpus"}, files: 4},
		{name: "yaml", seed: []string{"yaml-corpus"}, files: 2},
		{name: "xml", seed: []string{"xml-corpus"}, files: 2},
		{name: "protobuf", seed: []string{"protobuf-corpus"}, files: 3},
		{name: "dataset", seed: []string{"dataset-corpus"}, files: 2},
		{name: "dataset-xml", seed: []string{"dataset-corpus"}, edit: [3]string{"seeds/missions/mission-seeds.factory.yaml", "family: dataset", "family: dataset\n  options: { format: xml }"}, files: 2},
		{name: "dataset-csv", seed: []string{"dataset-corpus"}, edit: [3]string{"seeds/missions/mission-seeds.factory.yaml", "family: dataset", "family: dataset\n  options: { format: csv }"}, files: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.avro {
				withoutAvroOracle(t)
			}
			w := newWorkspace(t).seed(tc.seed...)
			if tc.edit[0] != "" {
				w.replace(tc.edit[0], tc.edit[1], tc.edit[2])
			}
			files := w.expand()
			goldenDir := filepath.Join(repoRoot(), "internal", "fixtures", "testdata", "expected", tc.name)
			compared := 0
			err := filepath.WalkDir(goldenDir, func(p string, d fs.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				rel, _ := filepath.Rel(goldenDir, p)
				rel = filepath.ToSlash(rel)
				want, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				compared++
				if got, ok := files[rel]; !ok {
					t.Errorf("%s: not produced (produced: %v)", rel, keysOf(files))
				} else if !bytes.Equal(got, want) {
					t.Errorf("%s differs from the expected output:\n--- want\n%s\n+++ got\n%s", rel, want, got)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if compared != tc.files {
				t.Errorf("compared %d golden files, want %d", compared, tc.files)
			}
			for rel := range files {
				if !strings.HasSuffix(rel, ".gitignore") {
					if _, err := os.Stat(filepath.Join(goldenDir, filepath.FromSlash(rel))); err != nil {
						t.Errorf("%s produced but not expected", rel)
					}
				}
			}
		})
	}
}

// TestExpectedAdoptions adopts corpus files and compares every spec file
// adoption writes with the expected output.
func TestExpectedAdoptions(t *testing.T) {
	cases := []struct {
		name, corpus, family, schema, glob, factory string
		files                                       int
	}{
		{"adopt-json", "json-corpus", "json", "schemas/telemetry.schema.json", "ingest/*.json", "ingested-telemetry", 5},
		{"adopt-yaml", "yaml-corpus", "yaml", "schemas/app-config.schema.json", "legacy/*.yaml", "legacy-configs", 4},
		{"adopt-dataset", "dataset-corpus", "dataset", "ddl/01-schema.sql", "features/return/seeds/*.yaml", "return-seeds", 4},
		{"adopt-protobuf", "protobuf-corpus", "protobuf", "schemas/orders.desc#axx.test.OrderEvent", "ingest/*.json", "ingested-events", 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorkspace(t).seed(tc.corpus)
			res, err := w.adopter().Adopt(tc.family, tc.schema, tc.glob, tc.factory, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(res.Written) != tc.files {
				t.Errorf("wrote %v", res.Written)
			}
			goldenDir := filepath.Join(repoRoot(), "internal", "fixtures", "testdata", "expected", tc.name)
			for _, rel := range res.Written {
				want, err := os.ReadFile(filepath.Join(goldenDir, filepath.FromSlash(rel)))
				if err != nil {
					t.Errorf("%s: written but not expected", rel)
					continue
				}
				if got := w.read(rel); got != string(want) {
					t.Errorf("%s differs from the expected output:\n--- want\n%s\n+++ got\n%s", rel, want, got)
				}
			}
		})
	}
}
