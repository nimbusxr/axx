package appcore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
)

type unlistableFiles struct{ core.Files }

func (unlistableFiles) List(context.Context, int) ([]string, error) { return nil, nil }

func TestAppFileIndexIncludesWritesAndClearsOnEmpty(t *testing.T) {
	sc := scenario()
	root := t.TempDir()
	if err := RegisterFiles(sc, "browser", func(path string) (core.Files, error) {
		return unlistableFiles{core.LocalFiles(filepath.Join(root, path))}, nil
	}); err != nil {
		t.Fatal(err)
	}
	files, err := Files(sc, "browser", "axx-models")
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Write(sc.Context(), "speech.axxmodel", []byte("stub")); err != nil {
		t.Fatal(err)
	}
	names, err := files.List(sc.Context(), 100)
	if err != nil || len(names) != 1 || names[0] != "speech.axxmodel" {
		t.Fatalf("an unlistable provider lost a written artifact: %v %v", names, err)
	}
	if err := files.Empty(sc.Context()); err != nil {
		t.Fatal(err)
	}
	if names, err := files.List(sc.Context(), 100); err != nil || len(names) != 0 {
		t.Fatalf("empty left the write index behind: %v %v", names, err)
	}
}

func TestAppFileProvidersAreIsolatedAndKeepNativeFiles(t *testing.T) {
	sc := scenario()
	root := t.TempDir()
	if err := RegisterFiles(sc, "browser", func(path string) (core.Files, error) { return core.LocalFiles(filepath.Join(root, path)), nil }); err != nil {
		t.Fatal(err)
	}
	f, err := Files(sc, "browser", "axx-models")
	if err != nil || f.Where() != filepath.Join(root, "axx-models") {
		t.Fatalf("browser files: %v %v", f, err)
	}
	if err := Register(sc, &App{Name: "native", Platform: "ios", Family: &fake{}}, true); err != nil {
		t.Fatal(err)
	}
	if f, err := Files(sc, "native", "axx-models"); err != nil || f.Where() != "/nowhere" {
		t.Fatalf("native files: %v %v", f, err)
	}
	if err := RegisterFiles(sc, "native", func(string) (core.Files, error) { return nil, nil }); err == nil {
		t.Fatal("file provider replaced native app")
	}
	if err := Register(sc, &App{Name: "browser", Platform: "ios", Family: &fake{}}, true); err == nil {
		t.Fatal("native app replaced file provider")
	}
	if _, err := Files(scenario(), "browser", "axx-models"); err == nil {
		t.Fatal("file provider leaked to another scenario")
	}
}
