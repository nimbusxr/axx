package schemas

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/schemacompat"
)

const contractDir = "../testdata/schemas"

var (
	updateContract = flag.Bool("update", false, "freeze what the schemas gained: copy them to testdata/schemas, when they still accept every file they accepted")
	breakContract  = flag.Bool("break", false, "freeze the schemas as they are, although they refuse what they accepted: for a change the owner decided on")
)

// TestSchemaContract asserts that the schemas of axx.yaml and the fixture spec files still accept
// every file their frozen copies (testdata/schemas) accept: a schema may grow, and may not take
// back what a file could say (ADR 0004). And that the copies are the schemas as they are, so that
// what they gained is frozen too: -update copies them, when nothing broke. A change that breaks,
// which the owner decided on, is frozen with -break.
func TestSchemaContract(t *testing.T) {
	names := Names()
	for _, name := range names {
		now, err := Read(name)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(contractDir, name)
		before, err := os.ReadFile(path)
		switch {
		case os.IsNotExist(err):
			if !*updateContract {
				t.Errorf("%s is not frozen yet: run `go test ./schemas -run TestSchemaContract -update`", name)
				continue
			}
		case err != nil:
			t.Fatal(err)
		case bytes.Equal(before, now):
			continue
		default:
			breaks, err := schemacompat.Breaks(before, now)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if len(breaks) > 0 && !*breakContract {
				t.Errorf("%s refuses what it accepted, or no longer knows it; a schema is a frozen contract (ADR 0004):\n  %s\n"+
					"if the owner decided on the change, run `go test ./schemas -run TestSchemaContract -break`", name, strings.Join(breaks, "\n  "))
				continue
			}
			if len(breaks) == 0 && !*updateContract {
				t.Errorf("%s gained something, and still accepts what it accepted: run `go test ./schemas -run TestSchemaContract -update` to freeze it", name)
				continue
			}
		}
		if err := os.MkdirAll(contractDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, now, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	frozen, err := filepath.Glob(filepath.Join(contractDir, "*.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range frozen {
		if !slices.Contains(names, filepath.Base(path)) {
			t.Errorf("%s is frozen, and axx no longer publishes it: a schema is a frozen contract (ADR 0004)", filepath.Base(path))
		}
	}
}
