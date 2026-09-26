package driver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// mirror serves a Node.js archive and a playwright-core tarball, as
// nodejs.org and the npm registry do, and counts the downloads.
type mirror struct {
	*httptest.Server
	files     map[string][]byte
	downloads atomic.Int32
}

func newMirror(t *testing.T, files map[string][]byte) *mirror {
	t.Helper()
	m := &mirror{files: files}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := m.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		m.downloads.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(m.Close)
	return m
}

// options points Ensure at m, with hashes of what m serves (or of want).
func options(t *testing.T, m *mirror, nodeArchive, core []byte, goos, goarch string) Options {
	t.Helper()
	platform, _, err := nodePlatform(goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		CacheDir: t.TempDir(), NodeMirror: m.URL + "/node", NPMRegistry: m.URL + "/npm", GOOS: goos, GOARCH: goarch,
		coreVersion: "9.9.9", nodeVersion: "1.2.3", coreIntegrity: integrity(core),
		nodeSums: map[string]string{platform: sha256Hex(nodeArchive)},
	}
}

func TestPreparesTheDriverOnce(t *testing.T) {
	node := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!node", "node-v1.2.3-linux-x64/README.md": "x"})
	core := tgz(t, map[string]string{"package/cli.js": "cli", "package/lib/coreBundle.js": "bundle"})
	m := newMirror(t, map[string][]byte{
		"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz":        node,
		"/npm/playwright-core/-/playwright-core-9.9.9.tgz": core,
	})
	o := options(t, m, node, core, "linux", "amd64")
	dir, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"node": "#!node", "package/cli.js": "cli", "package/lib/coreBundle.js": "bundle"} {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Errorf("%s: %q %v", name, got, err)
		}
	}
	if st, _ := os.Stat(filepath.Join(dir, "node")); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Error("node is not executable")
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		t.Error("only the node binary is kept from the Node.js archive")
	}
	// The second time, it is there.
	again, err := Ensure(context.Background(), o)
	if err != nil || again != dir {
		t.Fatalf("%s %v", again, err)
	}
	if n := m.downloads.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
	// No leftovers from the preparation.
	entries, _ := os.ReadDir(o.CacheDir)
	if len(entries) != 1 {
		t.Errorf("cache holds %d entries", len(entries))
	}
}

func TestWindowsTakesNodeFromTheZip(t *testing.T) {
	node := zipOf(t, map[string]string{"node-v1.2.3-win-x64/node.exe": "MZ"})
	core := tgz(t, map[string]string{"package/cli.js": "cli"})
	m := newMirror(t, map[string][]byte{
		"/node/v1.2.3/node-v1.2.3-win-x64.zip":             node,
		"/npm/playwright-core/-/playwright-core-9.9.9.tgz": core,
	})
	dir, err := Ensure(context.Background(), options(t, m, node, core, "windows", "amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "node.exe")); err != nil || string(b) != "MZ" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestTamperedDownloadsAreRefused(t *testing.T) {
	node := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!node"})
	core := tgz(t, map[string]string{"package/cli.js": "cli"})
	evil := tgz(t, map[string]string{"package/cli.js": "evil"})
	for name, tc := range map[string]struct {
		served map[string][]byte
		want   string
	}{
		"node": {map[string][]byte{"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz": evil}, "does not match the pinned"},
		"core": {map[string][]byte{
			"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz":        node,
			"/npm/playwright-core/-/playwright-core-9.9.9.tgz": evil,
		}, "integrity"},
	} {
		t.Run(name, func(t *testing.T) {
			m := newMirror(t, tc.served)
			o := options(t, m, node, core, "linux", "amd64")
			if _, err := Ensure(context.Background(), o); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v", err)
			}
			if entries, _ := os.ReadDir(o.CacheDir); len(entries) != 0 {
				t.Errorf("a refused download left %d entries in the cache", len(entries))
			}
		})
	}
}

func TestArchiveEntriesStayInTheDirectory(t *testing.T) {
	node := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!node"})
	core := tgz(t, map[string]string{"package/../../escape": "x"})
	m := newMirror(t, map[string][]byte{
		"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz":        node,
		"/npm/playwright-core/-/playwright-core-9.9.9.tgz": core,
	})
	if _, err := Ensure(context.Background(), options(t, m, node, core, "linux", "amd64")); err == nil || !strings.Contains(err.Error(), "leaves the directory") {
		t.Fatalf("got %v", err)
	}
}

func TestPlatforms(t *testing.T) {
	for _, p := range [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}} {
		platform, _, err := nodePlatform(p[0], p[1])
		if err != nil {
			t.Fatal(err)
		}
		if len(nodeSums[platform]) != 64 {
			t.Errorf("no pinned Node.js sum for %s", platform)
		}
	}
	if _, _, err := nodePlatform("linux", "arm"); err == nil || !strings.Contains(err.Error(), "linux/arm") {
		t.Errorf("got %v", err)
	}
	if !strings.HasPrefix(coreIntegrity, "sha512-") {
		t.Error("playwright-core integrity is not pinned")
	}
}
