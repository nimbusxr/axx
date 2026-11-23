package packbuild

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// module writes a module with a custom pack, a package beside it and a nested module, and
// gives what a build's key is made from for it.
func module(t *testing.T) (root string, locals map[string]local, write func(rel, body string)) {
	t.Helper()
	root = t.TempDir()
	write = func(rel, body string) {
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
	return root, map[string]local{"core": {dir: filepath.Join(root, "packs/core"), moduleRoot: root}}, write
}

func keyOf(t *testing.T, o Options, locals map[string]local) string {
	t.Helper()
	k, err := buildKey(o, locals)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// A custom pack builds with the rest of its module, so a change to a package beside it changes the
// build; a nested module is not part of it.
func TestBuildKeyCoversThePacksModule(t *testing.T) {
	_, locals, write := module(t)
	before := keyOf(t, Options{}, locals)
	write("examples/app/main.go", "package main\n\nfunc main() {}\n")
	if keyOf(t, Options{}, locals) != before {
		t.Error("a change to a nested module changed the build key")
	}
	write("protocol/protocol.go", "package protocol\n\nconst Version = 2\n")
	if keyOf(t, Options{}, locals) == before {
		t.Error("a change to a package beside the pack left the build key as it was")
	}
}

// A CI runner checks the project out and installs axx afresh for each run: every file has that
// run's time, and the checkout may be somewhere else. The key is the same, so the run finds the
// build the one before it kept; and it changes with what a file holds, whatever its size and time.
func TestBuildKeyIsTheSameOnAFreshCheckoutAndInstall(t *testing.T) {
	root, locals, write := module(t)
	axx := filepath.Join(t.TempDir(), "axx")
	if err := os.WriteFile(axx, []byte("the installed axx"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable = func() (string, error) { return axx, nil }
	t.Cleanup(func() { executable = os.Executable })
	release := Options{AxxVersion: "0.8.0"}
	before := keyOf(t, release, locals)

	// The same files, with another time each, and axx installed again.
	later := time.Now().Add(time.Hour)
	for _, rel := range []string{"go.mod", "packs/core/core.go", "protocol/protocol.go"} {
		if err := os.Chtimes(filepath.Join(root, rel), later, later); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(axx, later, later); err != nil {
		t.Fatal(err)
	}
	if keyOf(t, release, locals) != before {
		t.Error("the same files and release at another time have another build key")
	}

	// The same checkout somewhere else.
	elsewhere := t.TempDir()
	if err := os.CopyFS(elsewhere, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	moved := map[string]local{"core": {dir: filepath.Join(elsewhere, "packs/core"), moduleRoot: elsewhere}}
	if keyOf(t, release, moved) != before {
		t.Error("the same checkout in another directory has another build key")
	}

	// What a file holds, at the same size and time.
	p := filepath.Join(root, "packs/core/core.go")
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	write("packs/core/core.go", "package cor3\n")
	if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if keyOf(t, release, locals) == before {
		t.Error("a file that holds something else, at the same size and time, left the build key as it was")
	}
	if keyOf(t, Options{AxxVersion: "0.8.1"}, moved) == before {
		t.Error("another release has the same build key")
	}
}

// A build from a source directory is of all the directory holds, what its packs embed included:
// the axx built from it stands for that, so building axx again prepares the packs again.
func TestBuildKeyOfASourceBuildFollowsTheAxxBuiltFromIt(t *testing.T) {
	_, locals, _ := module(t)
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module "+AxxModule+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	axx := filepath.Join(t.TempDir(), "axx")
	if err := os.WriteFile(axx, []byte("axx, built from source"), 0o755); err != nil {
		t.Fatal(err)
	}
	executable = func() (string, error) { return axx, nil }
	t.Cleanup(func() { executable = os.Executable })
	dev := Options{AxxVersion: "0.0.0-dev", AxxSource: source}
	before := keyOf(t, dev, locals)
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(axx, later, later); err != nil {
		t.Fatal(err)
	}
	if keyOf(t, dev, locals) == before {
		t.Error("axx built again from its source left the build key as it was")
	}
}
