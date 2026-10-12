package video

import (
	"os"
	"path/filepath"
	"testing"
)

// A library another axx placed first is kept: two test packages of a run download it at once,
// and Windows cannot replace a library a process has loaded.
func TestALibraryPlacedFirstIsKept(t *testing.T) {
	dir := t.TempDir()
	tmp := filepath.Join(dir, ".openh264-1")
	if err := os.WriteFile(tmp, []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	// What cannot be replaced, on every OS: a folder with a file in it.
	first := filepath.Join(dir, "openh264.dll")
	if err := os.MkdirAll(filepath.Join(first, "in-use"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := place(tmp, first); err != nil {
		t.Errorf("the library placed first is not kept: %v", err)
	}
	if err := place(filepath.Join(dir, "none"), filepath.Join(dir, "missing", "openh264.dll")); err == nil {
		t.Error("a library placed nowhere is placed")
	}
}
