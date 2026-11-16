package packbuild

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/packset"
)

func TestInputs(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("steps/go.mod", "module example.test/steps\n")
	write("steps/go.sum", "")
	write("steps/pack.go", "package steps\n\nimport _ \"embed\"\n\n//go:embed hook.sh \"with space.txt\" all:assets\nvar files string\n")
	write("steps/pack_test.go", "package steps\n")
	write("steps/hook.sh", "echo\n")
	write("steps/with space.txt", "")
	write("steps/assets/a/b.txt", "")
	write("steps/assets/.hidden", "")
	write("steps/inner/helper.go", "package inner\n")
	write("steps/inner/fast_arm64.s", "")
	write("steps/README.md", "")
	write("steps/testdata/x.go", "package x\n")
	write("steps/other/go.mod", "module example.test/other\n")
	write("steps/other/other.go", "package other\n")
	write("unrelated/main.go", "package main\n")

	e, err := packset.Parse("./steps")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Inputs(dir, []packset.Entry{e})
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range got {
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			t.Fatal(err)
		}
		got[i] = filepath.ToSlash(rel)
	}
	want := []string{
		"steps/assets/.hidden", "steps/assets/a/b.txt", "steps/go.mod", "steps/go.sum", "steps/hook.sh",
		"steps/inner/fast_arm64.s", "steps/inner/helper.go", "steps/pack.go", "steps/with space.txt",
	}
	if !slices.Equal(got, want) {
		t.Errorf("inputs:\n got %q\nwant %q", got, want)
	}

	// A project with axx's packs only compiles nothing of its own.
	pub, err := packset.Parse("rest")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Inputs(dir, []packset.Entry{pub}); err != nil || len(got) != 0 {
		t.Errorf("axx's packs only: %q, %v", got, err)
	}
}
