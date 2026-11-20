package video

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
)

// A download the network fails once is tried again. A runner's lookup of Cisco's host failed
// once, and every test that keeps a video failed with it.
func TestADownloadTheNetworkFailsIsTriedAgain(t *testing.T) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	build, ok := openh264Builds[platform]
	if !ok {
		t.Skipf("Cisco has no build of OpenH264 for %s", platform)
	}
	// What stands in for Cisco's file: "the library", as bzip2 packs it.
	packed, err := hex.DecodeString("425a6839314159265359a4b2146d00000491804000326414202000310030200d3d456a40838f177245385090a4b2146d")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(packed)
	openh264Builds[platform] = struct{ file, sha256 string }{build.file, hex.EncodeToString(sum[:])}
	t.Cleanup(func() { openh264Builds[platform] = build })

	// The server drops the first connection without an answer, as a network that fails does.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		if r.URL.Path != "/"+build.file {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(packed)
	}))
	t.Cleanup(srv.Close)
	cisco := openh264URL
	openh264URL = srv.URL + "/"
	t.Cleanup(func() { openh264URL = cisco })

	path, err := fetch(t.TempDir())
	if err != nil {
		t.Fatalf("after one connection that failed: %v", err)
	}
	if lib, err := os.ReadFile(path); err != nil || string(lib) != "the library" {
		t.Errorf("the library downloaded is %q: %v", lib, err)
	}
	if n := requests.Load(); n != 2 {
		t.Errorf("%d requests, not the one that failed and the one after it", n)
	}
}

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
