package lighthouse

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestTheLockfilePinsLighthouse(t *testing.T) {
	pkgs, err := packagesOf(lockfile, unused)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]string{}
	for _, p := range pkgs {
		have[p.Path] = p.Version
		if strings.Contains(p.Path, "@sentry/") || strings.Contains(p.Path, "@opentelemetry/") {
			t.Errorf("%s is installed: only Sentry loads it", p.Path)
		}
	}
	if have["node_modules/lighthouse"] != lighthouseVersion {
		t.Errorf("the lockfile pins Lighthouse %q, not %s", have["node_modules/lighthouse"], lighthouseVersion)
	}
	if have["node_modules/puppeteer-core"] == "" {
		t.Error("the lockfile has no puppeteer-core, which the helper connects to the browser with")
	}
	var manifest struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, err := os.ReadFile("npm/package.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &manifest); err != nil || manifest.Dependencies["lighthouse"] != lighthouseVersion {
		t.Errorf("npm/package.json asks for Lighthouse %q, not %s", manifest.Dependencies["lighthouse"], lighthouseVersion)
	}
}

// fakeLock is a lockfile: the root needs a and @parcels/labels; a needs
// its own c, not the root's other one, and a peer and an optional package
// that are not installed; @parcels/labels needs telemetry, which needs
// tracer.
const fakeLock = `{
  "name": "shop", "lockfileVersion": 3,
  "packages": {
    "": {"name": "shop", "dependencies": {"a": "1.0.0", "@parcels/labels": "2.0.0"}, "devDependencies": {"jest": "30.0.0"}},
    "node_modules/a": {"version": "1.0.0", "integrity": "%s", "dependencies": {"c": "^3.0.0"}, "optionalDependencies": {"fsevents": "*"}, "peerDependencies": {"bufferutil": "*"}},
    "node_modules/a/node_modules/c": {"version": "3.1.0", "integrity": "%s"},
    "node_modules/c": {"version": "2.0.0", "integrity": "sha512-unused"},
    "node_modules/@parcels/labels": {"version": "2.0.0", "integrity": "%s", "dependencies": {"telemetry": "1.0.0"}},
    "node_modules/telemetry": {"version": "1.0.0", "integrity": "sha512-left-out", "dependencies": {"tracer": "1.0.0"}},
    "node_modules/tracer": {"version": "1.0.0", "integrity": "sha512-left-out"},
    "node_modules/jest": {"version": "30.0.0", "dev": true, "integrity": "sha512-dev"}
  }
}`

// registry is an npm registry of tarballs, by the path of their download.
type registry struct {
	*httptest.Server
	downloads atomic.Int32
}

func newRegistry(t *testing.T, tarballs map[string][]byte) *registry {
	t.Helper()
	r := &registry{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.downloads.Add(1)
		b, ok := tarballs[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(r.Close)
	return r
}

// tarball is an npm package's tarball of files, in top (package/, mostly).
func tarball(t *testing.T, top string, files map[string]string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		mode := int64(0o644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: top + name, Mode: mode, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(files[name]))
	}
	if err := tw.WriteHeader(&tar.Header{Name: top + "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return b.Bytes()
}

// fakePackages are fakeLock's packages, and a registry that has them.
func fakePackages(t *testing.T) ([]npmPackage, *registry, map[string][]byte) {
	t.Helper()
	tarballs := map[string][]byte{
		"/a/-/a-1.0.0.tgz":                    tarball(t, "package/", map[string]string{"package.json": `{"name":"a"}`, "bin/a.js": "#!/usr/bin/env node"}),
		"/c/-/c-3.1.0.tgz":                    tarball(t, "package/", map[string]string{"package.json": `{"name":"c"}`}),
		"/@parcels/labels/-/labels-2.0.0.tgz": tarball(t, "labels/", map[string]string{"index.js": "export const label = 'PX-4101';"}),
	}
	lock := fmt.Sprintf(fakeLock, integrity(tarballs["/a/-/a-1.0.0.tgz"]), integrity(tarballs["/c/-/c-3.1.0.tgz"]), integrity(tarballs["/@parcels/labels/-/labels-2.0.0.tgz"]))
	pkgs, err := packagesOf([]byte(lock), []string{"telemetry"})
	if err != nil {
		t.Fatal(err)
	}
	return pkgs, newRegistry(t, tarballs), tarballs
}

func TestTheLockfileIsReadAsNodeFindsPackages(t *testing.T) {
	pkgs, _, _ := fakePackages(t)
	var got []string
	for _, p := range pkgs {
		got = append(got, p.Path+" "+p.Name+"@"+p.Version)
	}
	want := []string{
		"node_modules/@parcels/labels @parcels/labels@2.0.0",
		"node_modules/a a@1.0.0",
		"node_modules/a/node_modules/c c@3.1.0",
	}
	if !slices.Equal(got, want) {
		t.Errorf("packages:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for lock, want := range map[string]string{
		`{"lockfileVersion": 1, "dependencies": {}}`:                                                   "the lockfile is of version 1, not 2 or 3",
		`{"lockfileVersion": 3, "packages": {"": {"dependencies": {"a": "1.0.0"}}}}`:                   "the lockfile lacks a, which the root package needs",
		`{"lockfileVersion": 3, "packages": {"": {"name": "shop", "dependencies": {"a": "1.0.0"}}}}`:   "the lockfile lacks a, which shop needs",
		`{"lockfileVersion": 3, "packages": {"": {"dependencies": {"a": "1"}}, "node_modules/a": {}}}`: "the lockfile has no version or no sha512 integrity for node_modules/a",
	} {
		if _, err := packagesOf([]byte(lock), nil); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", lock, err, want)
		}
	}
}

func TestPackagesAreInstalledOnce(t *testing.T) {
	pkgs, reg, _ := fakePackages(t)
	dir := filepath.Join(t.TempDir(), "web", installName("shop-1.0.0", pkgs))
	announced := 0
	for range 2 {
		if err := install(context.Background(), dir, reg.URL, pkgs, func() { announced++ }); err != nil {
			t.Fatal(err)
		}
	}
	if announced != 1 || reg.downloads.Load() != 3 {
		t.Errorf("announced %d times, %d downloads; want once, 3", announced, reg.downloads.Load())
	}
	for file, want := range map[string]string{
		"node_modules/a/package.json":                "{\"name\":\"a\"}",
		"node_modules/a/bin/a.js":                    "#!/usr/bin/env node",
		"node_modules/a/node_modules/c/package.json": "{\"name\":\"c\"}",
		"node_modules/@parcels/labels/index.js":      "export const label = 'PX-4101';",
	} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file))); err != nil || string(b) != want {
			t.Errorf("%s: %q %v, want %q", file, b, err, want)
		}
	}
	for _, left := range []string{"node_modules/c", "node_modules/telemetry", "node_modules/tracer", "node_modules/jest"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(left))); err == nil {
			t.Errorf("%s is installed", left)
		}
	}
	if st, err := os.Stat(filepath.Join(dir, "node_modules", "a", "bin", "a.js")); err != nil || st.Mode().Perm()&0o100 == 0 {
		t.Errorf("a.js is not executable: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "node_modules", "a", "link")); err == nil {
		t.Error("a link is installed")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dir), "prepare-*")); len(left) > 0 {
		t.Errorf("left over: %v", left)
	}
}

func TestPackagesInstalledSideBySideAppearOnce(t *testing.T) {
	pkgs, reg, _ := fakePackages(t)
	dir := filepath.Join(t.TempDir(), installName("shop-1.0.0", pkgs))
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { errs[i] = install(context.Background(), dir, reg.URL, pkgs, func() {}) })
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 || entries[0].Name() != filepath.Base(dir) {
		t.Errorf("the cache holds %v", entries)
	}
}

func TestAPackageThatIsNotAsPinnedIsRefused(t *testing.T) {
	pkgs, _, tarballs := fakePackages(t)
	tarballs["/c/-/c-3.1.0.tgz"] = tarball(t, "package/", map[string]string{"package.json": `{"name":"c","scripts":{"postinstall":"curl"}}`})
	reg := newRegistry(t, tarballs)
	cache := t.TempDir()
	dir := filepath.Join(cache, installName("shop-1.0.0", pkgs))
	err := install(context.Background(), dir, reg.URL, pkgs, func() {})
	if err == nil || !strings.HasPrefix(err.Error(), "c 3.1.0: integrity sha512-") || !strings.Contains(err.Error(), "does not match the pinned "+pkgs[2].Integrity) {
		t.Errorf("a changed package: %v", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("an install that failed left %v", entries)
	}
}

func TestAPackageThatWritesOutsideItsDirectoryIsRefused(t *testing.T) {
	evil := tarball(t, "package/", map[string]string{"../../../escaped.js": "boom"})
	pkgs := []npmPackage{{Path: "node_modules/evil", Name: "evil", Version: "1.0.0", Integrity: integrity(evil)}}
	reg := newRegistry(t, map[string][]byte{"/evil/-/evil-1.0.0.tgz": evil})
	cache := t.TempDir()
	err := install(context.Background(), filepath.Join(cache, "evil"), reg.URL, pkgs, func() {})
	if err == nil || err.Error() != `evil 1.0.0: archive entry "../../../escaped.js" leaves the directory` {
		t.Errorf("an entry out of the directory: %v", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("an install that failed left %v", entries)
	}
}

func TestAMissingPackageSaysWhereItWasLookedFor(t *testing.T) {
	pkgs, _, tarballs := fakePackages(t)
	delete(tarballs, "/@parcels/labels/-/labels-2.0.0.tgz")
	reg := newRegistry(t, tarballs)
	err := install(context.Background(), filepath.Join(t.TempDir(), "shop"), reg.URL, pkgs, func() {})
	if err == nil || err.Error() != "download "+reg.URL+"/@parcels/labels/-/labels-2.0.0.tgz: 404 Not Found" {
		t.Errorf("a missing package: %v", err)
	}
}
