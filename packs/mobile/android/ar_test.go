package mobileandroid

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	appcore "github.com/nimbusxr/axx/packs/app/core"
	mobilecore "github.com/nimbusxr/axx/packs/mobile/core"
	"github.com/nimbusxr/axx/packs/mobile/internal/scene"
)

func writeAPK(t *testing.T, dex map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "app.apk")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for name, body := range dex {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// An app has ARCore when one of its dex files names ARCore's session: axx's AR agent attaches
// to it, and to no other.
func TestHasARCore(t *testing.T) {
	h := cloudtest.New(t, appcore.Pack(), mobilecore.Pack(), Pack())
	for _, c := range []struct {
		dex  map[string]string
		want bool
	}{
		{map[string]string{"classes.dex": "dex\n035 Lexample/Main;", "classes2.dex": "dex\n035 Lcom/google/ar/core/Session; Lcom/google/ar/core/Frame;"}, true},
		{map[string]string{"classes.dex": "dex\n035 Lexample/Main; Landroidx/camera/core/Preview;"}, false},
		{map[string]string{"classes.dex": "dex\n035", "assets/Lcom/google/ar/core/Session;.txt": "not a dex file"}, false},
	} {
		got, err := hasARCore(h.SC, writeAPK(t, c.dex))
		if err != nil || got != c.want {
			t.Errorf("hasARCore(%v) = %v, %v; want %v", c.dex, got, err, c.want)
		}
	}
}

// The bridge reads a pose as its 16 numbers, column by column, as the scene keeps them.
func TestPoseLine(t *testing.T) {
	m := scene.Identity
	m[12], m[13], m[14] = 0.5, 1.25, -3
	if got, want := poseLine(m), "1 0 0 0 0 1 0 0 0 0 1 0 0.5 1.25 -3 1"; got != want {
		t.Errorf("poseLine = %q, want %q", got, want)
	}
}
