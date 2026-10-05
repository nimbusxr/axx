package fixtures

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/iskorotkov/avro/v2"

	"github.com/nimbusxr/axx/internal/avrojson"
)

// repoRoot is the module root.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// copyTree copies src into dst: the files a checkout has (git's tracked and
// unignored files), so what builds leave in an example (an app's
// node_modules, its build folders) stays behind.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", src, "ls-files", "-co", "--exclude-standard", "-z", ".").Output(); err == nil {
		for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
			if rel == "" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(rel)))
			if errors.Is(err, fs.ErrNotExist) {
				continue // deleted in the working tree
			}
			if err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(dst, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// avroOracleAvailable reports whether internal/avrojson is the real decoder
// or still the placeholder written against its API.
func avroOracleAvailable() bool {
	s, err := avro.Parse(`{"type":"record","name":"Probe","fields":[{"name":"a","type":"string"}]}`)
	if err != nil {
		return false
	}
	_, err = avrojson.Decode(s, []byte(`{"a":"x"}`), avrojson.Options{})
	return err == nil
}

// withoutAvroOracle runs the avro family without its conformance oracle when
// internal/avrojson is still the placeholder, so byte-level tests still run;
// tests that exercise the oracle itself skip instead (skipWithoutAvroOracle).
func withoutAvroOracle(t *testing.T) {
	t.Helper()
	if avroOracleAvailable() {
		return
	}
	prev := avroDecode
	avroDecode = func(avro.Schema, []byte, avrojson.Options) (any, error) { return nil, nil }
	t.Cleanup(func() { avroDecode = prev })
}

func skipWithoutAvroOracle(t *testing.T) {
	t.Helper()
	if !avroOracleAvailable() {
		t.Skip("internal/avrojson is the placeholder stub (the real decoder lands with the Kafka pack); this test needs the Avro JSON oracle")
	}
}

func mustContain(t *testing.T, s string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(s, p) {
			t.Errorf("missing %q in:\n%s", p, s)
		}
	}
}

var errUnexpectedSuccess = errors.New("expected an error")
