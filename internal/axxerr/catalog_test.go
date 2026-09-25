package axxerr

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCatalogCoversSource keeps the catalog and the codes used in the
// source in sync, in both directions.
func TestCatalogCoversSource(t *testing.T) {
	root := filepath.Join("..", "..")
	re := regexp.MustCompile(`"(AXX-E\d{4})"`)
	used := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "docs", "sdk", ".claude", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.HasSuffix(p, "catalog.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(b), -1) {
			used[m[1]] = p
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for code, file := range used {
		if _, ok := Lookup(code); !ok {
			t.Errorf("%s (used in %s) has no catalog entry in internal/axxerr/catalog.go", code, file)
		}
	}
	for _, e := range Catalog() {
		if _, ok := used[e.Code]; !ok {
			t.Errorf("catalog entry %s is not used anywhere; remove it", e.Code)
		}
		if e.Title == "" || e.Meaning == "" || e.Fix == "" {
			t.Errorf("%s: title, meaning and fix are required", e.Code)
		}
		inRange := false
		for _, r := range Ranges {
			if e.Code >= r.From && e.Code <= r.To {
				inRange = true
			}
		}
		if !inRange {
			t.Errorf("%s is outside every documented range", e.Code)
		}
	}
}
