package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/version"
)

func TestWithoutFlag(t *testing.T) {
	got := withoutFlag([]string{"run", "--debug-steps", "features/a.feature:3", "--debug-steps=2346", "--debug=api"}, "--debug-steps")
	if strings.Join(got, " ") != "run features/a.feature:3 --debug=api" {
		t.Errorf("args %q", got)
	}
}

func TestReadExit(t *testing.T) {
	f := filepath.Join(t.TempDir(), "exit")
	if _, ok := readExit(f); ok {
		t.Error("a missing file has no exit code")
	}
	if err := os.WriteFile(f, []byte("3"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, ok := readExit(f); !ok || code != 3 {
		t.Errorf("exit %d, %v", code, ok)
	}
}

// A development build finds the checkout it was built from.
func TestAxxSource(t *testing.T) {
	t.Setenv("AXX_SOURCE_DIR", "")
	root := axxSource(version.Info{Channel: "dev"})
	if root == "" {
		t.Fatal("no checkout found")
	}
	if _, err := os.Stat(filepath.Join(root, "internal", "cli", "debugsteps.go")); err != nil {
		t.Errorf("checkout %s: %v", root, err)
	}
	if got := axxSource(version.Info{Channel: "release"}); got != "" {
		t.Errorf("a release build uses the module, got %s", got)
	}
	t.Setenv("AXX_SOURCE_DIR", "/src/axx")
	if got := axxSource(version.Info{Channel: "release"}); got != "/src/axx" {
		t.Errorf("AXX_SOURCE_DIR ignored: %s", got)
	}
}

// A pack created by a development build uses that build's checkout; one
// created by a release requires the release.
func TestPackGoMod(t *testing.T) {
	dev := packGoMod("steps", version.Info{Version: "0.0.0-dev", Channel: "dev"}, "/src/axx")
	if !strings.Contains(dev, "require github.com/nimbusxr/axx v0.0.0\n") || !strings.Contains(dev, "replace github.com/nimbusxr/axx => /src/axx\n") {
		t.Errorf("dev go.mod:\n%s", dev)
	}
	rel := packGoMod("steps", version.Info{Version: "0.1.0", Channel: "beta"}, "")
	if !strings.Contains(rel, "require github.com/nimbusxr/axx v0.1.0\n") || strings.Contains(rel, "replace") {
		t.Errorf("release go.mod:\n%s", rel)
	}
}
