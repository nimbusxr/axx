package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/config"
)

// A step's file is looked for next to its feature file first, then in the
// resources directories, then in axx.yaml's directory.
func TestFilesAreFoundNextToTheFeatureFirst(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"features/home/seeds/wen.yaml":   "beside the feature",
		"features/home/home-ios.feature": "Feature: x\n",
		"data/seeds/wen.yaml":            "in resources",
		"data/seeds/kofi.yaml":           "in resources",
		"seeds/ana.yaml":                 "beside axx.yaml",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e, err := New(Options{Config: &config.Config{Dir: dir, Resources: []string{"data"}}})
	if err != nil {
		t.Fatal(err)
	}
	feature := filepath.Join(dir, "features", "home")
	read := func(from, p string) string {
		t.Helper()
		path, err := e.ResolvePathFrom(from, p)
		if err != nil {
			t.Fatalf("%s from %q: %v", p, from, err)
		}
		b, _ := os.ReadFile(path)
		return string(b)
	}
	for _, c := range []struct{ from, path, want string }{
		{feature, "seeds/wen.yaml", "beside the feature"},
		{feature, "seeds/kofi.yaml", "in resources"},
		{feature, "seeds/ana.yaml", "beside axx.yaml"},
		{feature, "classpath:seeds/wen.yaml", "beside the feature"},
		{"", "seeds/wen.yaml", "in resources"}, // no feature file: as before
	} {
		if got := read(c.from, c.path); got != c.want {
			t.Errorf("%s from %q: %q, want %q", c.path, c.from, got, c.want)
		}
	}
	if _, err := e.ResolvePath("seeds/wen.yaml"); err != nil {
		t.Errorf("ResolvePath keeps its roots: %v", err)
	}

	// A file found nowhere says where it was looked for, the feature's directory first.
	_, err = e.ResolvePathFrom(feature, "seeds/missing.yaml")
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != CodeResource {
		t.Fatalf("err = %v", err)
	}
	looked := []string{
		filepath.Join(feature, "seeds", "missing.yaml"),
		filepath.Join(dir, "data", "seeds", "missing.yaml"),
		filepath.Join(dir, "seeds", "missing.yaml"),
	}
	if !strings.Contains(ae.Hint, strings.Join(looked, ", ")) || !strings.Contains(ae.Hint, "next to the feature file") {
		t.Errorf("hint: %s", ae.Hint)
	}
}
