package mobileandroid

import (
	"os"
	"testing"
)

// The couriers' app's manifest, as its debug build keeps it (UTF-8 strings).
func TestManifestPackage(t *testing.T) {
	b, err := os.ReadFile("testdata/courier-manifest.bin")
	if err != nil {
		t.Fatal(err)
	}
	if pkg, err := manifestPackage(b); err != nil || pkg != "example.parcels.courier" {
		t.Errorf("package %q, %v", pkg, err)
	}
	if _, err := manifestPackage([]byte("<manifest/>")); err == nil {
		t.Error("text XML read as binary")
	}
}

func TestUTF16PoolStrings(t *testing.T) {
	// "pkg" in UTF-16: its length, then its units.
	if s, err := poolString([]byte{3, 0, 'p', 0, 'k', 0, 'g', 0, 0, 0}, false); err != nil || s != "pkg" {
		t.Errorf("%q, %v", s, err)
	}
}
