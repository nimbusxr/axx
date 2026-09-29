package npm

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func tgz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		body := files[name]
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

// mirror serves files by their path, as nodejs.org does, and counts the
// downloads.
type mirror struct {
	*httptest.Server
	downloads atomic.Int32
}

func newMirror(t *testing.T, files map[string][]byte) *mirror {
	t.Helper()
	m := &mirror{}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[r.URL.Path]
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

// nodeOptions points EnsureNode at m, for Node.js 1.2.3, whose archive for
// the platform is archive.
func nodeOptions(t *testing.T, m *mirror, archive []byte, goos, goarch string) Options {
	t.Helper()
	platform, err := NodePlatform(goos, goarch)
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		CacheDir: t.TempDir(), NodeMirror: m.URL + "/node", GOOS: goos, GOARCH: goarch,
		Node: NodeRelease{Version: "1.2.3", Sums: map[string]string{platform: sha256Hex(archive)}},
	}
}

func TestNodeIsDownloadedOnce(t *testing.T) {
	archive := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!node", "node-v1.2.3-linux-x64/README.md": "x"})
	m := newMirror(t, map[string][]byte{"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz": archive})
	o := nodeOptions(t, m, archive, "linux", "amd64")
	node, err := EnsureNode(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(o.CacheDir, "node-v1.2.3-linux-x64", "node"); node != want {
		t.Errorf("node is %s, want %s", node, want)
	}
	if b, err := os.ReadFile(node); err != nil || string(b) != "#!node" {
		t.Errorf("%q %v", b, err)
	}
	if st, _ := os.Stat(node); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Error("node is not executable")
	}
	if entries, _ := os.ReadDir(filepath.Dir(node)); len(entries) != 1 {
		t.Errorf("only the node binary is kept from the Node.js archive: %v", entries)
	}
	// The second time, it is there.
	again, err := EnsureNode(context.Background(), o)
	if err != nil || again != node {
		t.Fatalf("%s %v", again, err)
	}
	if n := m.downloads.Load(); n != 1 {
		t.Errorf("%d downloads, want 1", n)
	}
	// No leftovers from the preparation.
	if entries, _ := os.ReadDir(o.CacheDir); len(entries) != 1 {
		t.Errorf("cache holds %v", entries)
	}
}

func TestWindowsTakesNodeFromTheZip(t *testing.T) {
	archive := zipOf(t, map[string]string{"node-v1.2.3-win-x64/node.exe": "MZ", "node-v1.2.3-win-x64/npm.cmd": "npm"})
	m := newMirror(t, map[string][]byte{"/node/v1.2.3/node-v1.2.3-win-x64.zip": archive})
	node, err := EnsureNode(context.Background(), nodeOptions(t, m, archive, "windows", "amd64"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(node) != "node.exe" {
		t.Errorf("node is %s", node)
	}
	if b, err := os.ReadFile(node); err != nil || string(b) != "MZ" {
		t.Fatalf("%q %v", b, err)
	}
}

// WriteNode puts the binary alone in a directory of the caller's, which
// holds Node.js beside what it runs.
func TestNodeIsWrittenIntoADirectory(t *testing.T) {
	archive := tgz(t, map[string]string{"node-v1.2.3-darwin-arm64/bin/node": "#!node", "node-v1.2.3-darwin-arm64/lib/x.js": "x"})
	m := newMirror(t, map[string][]byte{"/node/v1.2.3/node-v1.2.3-darwin-arm64.tar.gz": archive})
	o := nodeOptions(t, m, archive, "darwin", "arm64")
	dir := t.TempDir()
	node, err := WriteNode(context.Background(), o, dir)
	if err != nil || node != filepath.Join(dir, "node") {
		t.Fatalf("%s %v", node, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("the directory holds %v", entries)
	}
	if entries, _ := os.ReadDir(o.CacheDir); len(entries) != 0 {
		t.Errorf("the cache holds %v", entries)
	}
}

func TestATamperedNodeIsRefused(t *testing.T) {
	archive := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!node"})
	evil := tgz(t, map[string]string{"node-v1.2.3-linux-x64/bin/node": "#!evil"})
	m := newMirror(t, map[string][]byte{"/node/v1.2.3/node-v1.2.3-linux-x64.tar.gz": evil})
	o := nodeOptions(t, m, archive, "linux", "amd64")
	_, err := EnsureNode(context.Background(), o)
	if err == nil || err.Error() != "node-v1.2.3-linux-x64.tar.gz: SHA-256 "+sha256Hex(evil)+" does not match the pinned "+sha256Hex(archive) {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(o.CacheDir); len(entries) != 0 {
		t.Errorf("a refused download left %v in the cache", entries)
	}
}

func TestNodeMirrorPointsTheDownloadElsewhere(t *testing.T) {
	archive := tgz(t, map[string]string{"node-v1.2.3-linux-arm64/bin/node": "#!node"})
	m := newMirror(t, map[string][]byte{"/dist/v1.2.3/node-v1.2.3-linux-arm64.tar.gz": archive})
	t.Setenv("NODE_MIRROR", m.URL+"/dist/")
	o := nodeOptions(t, m, archive, "linux", "arm64")
	o.NodeMirror = ""
	if _, err := EnsureNode(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n := m.downloads.Load(); n != 1 {
		t.Errorf("%d downloads from the mirror", n)
	}
}

func TestNodePlatforms(t *testing.T) {
	for _, p := range [][3]string{
		{"linux", "amd64", "linux-x64"},
		{"linux", "arm64", "linux-arm64"},
		{"darwin", "amd64", "darwin-x64"},
		{"darwin", "arm64", "darwin-arm64"},
		{"windows", "amd64", "win-x64"},
		{"windows", "arm64", "win-arm64"},
	} {
		platform, err := NodePlatform(p[0], p[1])
		if err != nil || platform != p[2] {
			t.Errorf("%s/%s: %s %v, want %s", p[0], p[1], platform, err, p[2])
		}
		if len(nodeSums[platform]) != 64 {
			t.Errorf("no pinned Node.js sum for %s", platform)
		}
	}
	if len(nodeSums) != 6 {
		t.Errorf("%d pinned sums", len(nodeSums))
	}
	if _, err := NodePlatform("linux", "arm"); err == nil || err.Error() != "there is no Node.js for linux/arm" {
		t.Errorf("got %v", err)
	}
	if NodeName("windows") != "node.exe" || NodeName("linux") != "node" {
		t.Error("the binary's name")
	}
	if o := (Options{}).defaults(); o.Node.Version != NodeVersion || o.NodeMirror != "https://nodejs.org/dist" && os.Getenv("NODE_MIRROR") == "" {
		t.Errorf("defaults: %+v", o)
	}
}

func TestCacheDirAndRegistry(t *testing.T) {
	base, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := CacheDir("web"); got != filepath.Join(base, "axx", "web") {
		t.Errorf("CacheDir: %s", got)
	}
	t.Setenv("npm_config_registry", "")
	if got := Registry(); got != "https://registry.npmjs.org" {
		t.Errorf("Registry: %s", got)
	}
	t.Setenv("npm_config_registry", "https://npm.parcels.example/")
	if got := Registry(); got != "https://npm.parcels.example" {
		t.Errorf("Registry: %s", got)
	}
}

func TestUnpackingFiles(t *testing.T) {
	archive := tgz(t, map[string]string{"package/axe.min.js": "axe", "package/../../escape": "x"})
	dest := filepath.Join(t.TempDir(), "a", "axe.min.js")
	if err := UnpackFile(archive, "package/axe.min.js", dest); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "axe" {
		t.Errorf("%q %v", b, err)
	}
	if err := UnpackFile(archive, "package/axe.js", dest); err == nil || err.Error() != "package/axe.js is not in the archive" {
		t.Errorf("a missing file: %v", err)
	}
	dir := t.TempDir()
	if err := Unpack(archive, filepath.Join(dir, "package")); err == nil || err.Error() != `archive entry "../../escape" leaves the directory` {
		t.Errorf("an entry out of the directory: %v", err)
	}
	if err := Unpack([]byte("not gzip"), dir); err == nil {
		t.Error("a tarball that is not one")
	}
}

func TestUnzippingAnApp(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct {
		name string
		mode os.FileMode
	}{
		{"Courier.app/Courier", 0o755},
		{"Courier.app/Info.plist", 0o644},
		{"Courier.app/Frameworks/", os.ModeDir | 0o755},
		{"Courier.app/Current", os.ModeSymlink | 0o777},
	} {
		h := &zip.FileHeader{Name: f.name}
		h.SetMode(f.mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(f.name))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := Unzip(buf.Bytes(), dir); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Stat(filepath.Join(dir, "Courier.app", "Courier"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && exe.Mode().Perm() != 0o755 {
		t.Errorf("the app's executable is %v", exe.Mode())
	}
	if b, err := os.ReadFile(filepath.Join(dir, "Courier.app", "Info.plist")); err != nil || string(b) != "Courier.app/Info.plist" {
		t.Errorf("%q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "Courier.app", "Current")); !os.IsNotExist(err) {
		t.Errorf("a link was unpacked: %v", err)
	}
	escape := zipOf(t, map[string]string{"../escape": "x"})
	if err := Unzip(escape, t.TempDir()); err == nil || err.Error() != `archive entry "../escape" leaves the directory` {
		t.Errorf("an entry out of the directory: %v", err)
	}
	if err := Unzip([]byte("not a zip"), dir); err == nil {
		t.Error("an archive that is not one")
	}
}

func TestWriteOnceKeepsTheFirst(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "helper.mjs")
	for _, content := range []string{"first", "second"} {
		if err := WriteOnce(dest, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if b, err := os.ReadFile(dest); err != nil || string(b) != "first" {
		t.Errorf("%q %v", b, err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(dest)); len(entries) != 1 || strings.HasPrefix(entries[0].Name(), "write-") {
		t.Errorf("left over: %v", entries)
	}
}
