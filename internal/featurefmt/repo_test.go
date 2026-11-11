package featurefmt

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoFeatures are the feature files of axx's repository, as `axx fmt`
// would find them (no folder whose name starts with a dot, no
// node_modules), but those of tests' testdata, which are laid out as their
// tests need.
func repoFeatures(t *testing.T) []string {
	t.Helper()
	root := filepath.Join("..", "..")
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "node_modules" || d.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !d.IsDir() && strings.HasSuffix(d.Name(), ".feature") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 50 {
		t.Fatalf("found %d feature files in the repository, fewer than it has", len(files))
	}
	return files
}

// Every feature of the repository lays out without changing what it says,
// and laying it out again changes nothing.
func TestRepositoryFeaturesKeepTheirMeaning(t *testing.T) {
	for _, f := range repoFeatures(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Format(f, src, Options{})
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		again, err := Format(f, out, Options{})
		if err != nil || !bytes.Equal(again, out) {
			t.Errorf("%s: laid out again, it changes (%v)", f, err)
		}
	}
}

// The repository's features stay laid out, as CI checks.
func TestRepositoryFeaturesAreLaidOut(t *testing.T) {
	var unformatted []string
	for _, f := range repoFeatures(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		out, err := Format(f, src, Options{})
		if err == nil && !bytes.Equal(out, src) {
			rel, _ := filepath.Rel(filepath.Join("..", ".."), f)
			unformatted = append(unformatted, filepath.ToSlash(rel))
		}
	}
	if len(unformatted) > 0 {
		t.Errorf("these features are not laid out; run `go run ./cmd/axx fmt %s` from the repository's root:\n  %s",
			strings.Join(unformatted, " "), strings.Join(unformatted, "\n  "))
	}
}

// The indent changes only how far in things sit: every feature of the
// repository laid out four spaces to a level, then two, is what two gives.
func TestRepositoryFeaturesChangeOnlyTheirIndent(t *testing.T) {
	for _, f := range repoFeatures(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		four, err := Format(f, src, Options{Indent: 4})
		if err != nil {
			t.Errorf("%s: indent 4: %v", f, err)
			continue
		}
		back, err := Format(f, four, Options{Indent: 2})
		if err != nil {
			t.Errorf("%s: back to 2: %v", f, err)
			continue
		}
		two, err := Format(f, src, Options{Indent: 2})
		if err != nil || !bytes.Equal(back, two) {
			t.Errorf("%s: laid out with 4 then 2 differs from 2 (%v)", f, err)
		}
	}
}
