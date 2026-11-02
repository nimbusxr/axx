package packbuild

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A custom pack builds with the rest of its module, so a change to a package beside it changes the
// build; a nested module is not part of it.
func TestBuildKeyCoversThePacksModule(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/m\n")
	write("packs/core/core.go", "package core\n")
	write("protocol/protocol.go", "package protocol\n")
	write("examples/go.mod", "module example.com/m/examples\n")
	write("examples/app/main.go", "package main\n")
	locals := map[string]local{"core": {dir: filepath.Join(root, "packs/core"), moduleRoot: root}}

	key := func() string {
		t.Helper()
		k, err := buildKey(Options{}, locals)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	touch := func(rel string) {
		t.Helper()
		later := time.Now().Add(time.Minute)
		if err := os.Chtimes(filepath.Join(root, rel), later, later); err != nil {
			t.Fatal(err)
		}
	}

	before := key()
	touch("examples/app/main.go")
	if key() != before {
		t.Error("a change to a nested module changed the build key")
	}
	touch("protocol/protocol.go")
	if key() == before {
		t.Error("a change to a package beside the pack left the build key as it was")
	}
}
