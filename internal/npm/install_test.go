package npm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

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

var linux = platformOf("linux", "amd64")

// fakePackages are fakeLock's packages, and a registry that has them.
func fakePackages(t *testing.T) ([]Package, *registry, map[string][]byte) {
	t.Helper()
	tarballs := map[string][]byte{
		"/a/-/a-1.0.0.tgz":                    tarball(t, "package/", map[string]string{"package.json": `{"name":"a"}`, "bin/a.js": "#!/usr/bin/env node"}),
		"/c/-/c-3.1.0.tgz":                    tarball(t, "package/", map[string]string{"package.json": `{"name":"c"}`}),
		"/@parcels/labels/-/labels-2.0.0.tgz": tarball(t, "labels/", map[string]string{"index.js": "export const label = 'PX-4101';"}),
	}
	pkgs, err := packagesOf([]byte(fakeLockOf(tarballs)), []string{"telemetry"}, linux)
	if err != nil {
		t.Fatal(err)
	}
	return pkgs, newRegistry(t, tarballs), tarballs
}

func fakeLockOf(tarballs map[string][]byte) string {
	return fmt.Sprintf(fakeLock, Integrity(tarballs["/a/-/a-1.0.0.tgz"]), Integrity(tarballs["/c/-/c-3.1.0.tgz"]), Integrity(tarballs["/@parcels/labels/-/labels-2.0.0.tgz"]))
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
		`{"lockfileVersion": 1, "dependencies": {}}`:                                                                       "the lockfile is of version 1, not 2 or 3",
		`{"lockfileVersion": 3, "packages": {"": {"dependencies": {"a": "1.0.0"}}}}`:                                       "the lockfile lacks a, which the root package needs",
		`{"lockfileVersion": 3, "packages": {"": {"name": "shop", "dependencies": {"a": "1.0.0"}}}}`:                       "the lockfile lacks a, which shop needs",
		`{"lockfileVersion": 3, "packages": {"": {"dependencies": {"a": "1"}}, "node_modules/a": {}}}`:                     "the lockfile has no version or no sha512 integrity for node_modules/a",
		`{"lockfileVersion": 3, "packages": {}}`:                                                                           "the lockfile has no root package",
		`{"lockfileVersion": 3, "packages": {"": {"dependencies": {"a": "file:../a"}}, "node_modules/a": {"link": true}}}`: "the lockfile links node_modules/a to a directory: only packages of a registry can be installed",
	} {
		if _, err := packagesOf([]byte(lock), nil, linux); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", lock, err, want)
		}
	}
}

// A package's bundled dependencies come in its own tarball: they are not
// downloaded.
func TestBundledPackagesComeWithTheirBundler(t *testing.T) {
	lock := `{"lockfileVersion": 3, "packages": {
		"": {"dependencies": {"driver": "8.7.0"}},
		"node_modules/driver": {"version": "8.7.0", "integrity": "sha512-ZHJpdmVy", "bundleDependencies": ["css"], "dependencies": {"css": "1.0.7"}},
		"node_modules/driver/node_modules/css": {"version": "1.0.7", "inBundle": true, "dependencies": {"parser": "^3.3.0"}},
		"node_modules/driver/node_modules/parser": {"version": "3.3.1", "inBundle": true}
	}}`
	pkgs, err := packagesOf([]byte(lock), nil, linux)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].Name != "driver" {
		t.Errorf("packages: %+v", pkgs)
	}
}

func TestPackagesAreInstalledOnce(t *testing.T) {
	pkgs, reg, _ := fakePackages(t)
	dir := filepath.Join(t.TempDir(), "web", installName("shop-1.0.0", pkgs, nil))
	announced := 0
	for range 2 {
		if err := install(context.Background(), dir, reg.URL, pkgs, nil, func() { announced++ }); err != nil {
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
	if st, err := os.Stat(filepath.Join(dir, "node_modules", "a", "bin", "a.js")); err != nil || runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
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
	dir := filepath.Join(t.TempDir(), installName("shop-1.0.0", pkgs, nil))
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Go(func() { errs[i] = install(context.Background(), dir, reg.URL, pkgs, nil, func() {}) })
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
	dir := filepath.Join(cache, installName("shop-1.0.0", pkgs, nil))
	err := install(context.Background(), dir, reg.URL, pkgs, nil, func() {})
	if err == nil || !strings.HasPrefix(err.Error(), "c 3.1.0: integrity sha512-") || !strings.Contains(err.Error(), "does not match the pinned "+pkgs[2].Integrity) {
		t.Errorf("a changed package: %v", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("an install that failed left %v", entries)
	}
}

func TestAPackageThatWritesOutsideItsDirectoryIsRefused(t *testing.T) {
	evil := tarball(t, "package/", map[string]string{"../../../escaped.js": "boom"})
	pkgs := []Package{{Path: "node_modules/evil", Name: "evil", Version: "1.0.0", Integrity: Integrity(evil)}}
	reg := newRegistry(t, map[string][]byte{"/evil/-/evil-1.0.0.tgz": evil})
	cache := t.TempDir()
	err := install(context.Background(), filepath.Join(cache, "evil"), reg.URL, pkgs, nil, func() {})
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
	err := install(context.Background(), filepath.Join(t.TempDir(), "shop"), reg.URL, pkgs, nil, func() {})
	if err == nil || err.Error() != "download "+reg.URL+"/@parcels/labels/-/labels-2.0.0.tgz: 404 Not Found" {
		t.Errorf("a missing package: %v", err)
	}
}

// Install names its directory after the packages, in the cache, or takes
// the one it is given.
func TestInstallPutsThePackagesInTheirDirectory(t *testing.T) {
	pkgs, reg, tarballs := fakePackages(t)
	cache := t.TempDir()
	o := InstallOptions{
		Lock: []byte(fakeLockOf(tarballs)), LeaveOut: []string{"telemetry"},
		CacheDir: cache, Name: "shop-1.0.0", Registry: reg.URL + "/", GOOS: "linux", GOARCH: "amd64",
	}
	dir, err := Install(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(cache, installName("shop-1.0.0", pkgs, nil)); dir != want {
		t.Errorf("installed in %s, want %s", dir, want)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@parcels", "labels", "index.js")); err != nil {
		t.Error(err)
	}
	o.Dir = filepath.Join(t.TempDir(), "shop")
	if dir, err := Install(context.Background(), o); err != nil || dir != o.Dir {
		t.Errorf("%s %v, want %s", dir, err, o.Dir)
	}
	o.Dir, o.Name = "", ""
	if _, err := Install(context.Background(), o); err == nil || err.Error() != "npm.Install needs a Dir, or a Name to name one after" {
		t.Errorf("no directory: %v", err)
	}
	o.Lock = []byte(`{"lockfileVersion": 1}`)
	if _, err := Install(context.Background(), o); err == nil || err.Error() != "the lockfile is of version 1, not 2 or 3" {
		t.Errorf("a bad lockfile: %v", err)
	}
}

// sharpLock is a lockfile with a package built for each platform, as sharp
// has them: optional dependencies, each for an os, a cpu and, on Linux, a
// libc. cli needs a package for every os but Windows ("!win32").
const sharpLock = `{
  "name": "labels", "lockfileVersion": 3,
  "packages": {
    "": {"name": "labels", "dependencies": {"sharp": "0.34.4", "cli": "1.0.0"}},
    "node_modules/sharp": {"version": "0.34.4", "integrity": "sha512-sharp", "hasInstallScript": true,
      "dependencies": {"detect-libc": "^2.1.0"},
      "optionalDependencies": {"@img/sharp-darwin-arm64": "0.34.4", "@img/sharp-darwin-x64": "0.34.4", "@img/sharp-linux-x64": "0.34.4", "@img/sharp-linuxmusl-x64": "0.34.4", "@img/sharp-win32-x64": "0.34.4"}},
    "node_modules/detect-libc": {"version": "2.1.0", "integrity": "sha512-detect-libc"},
    "node_modules/@img/sharp-darwin-arm64": {"version": "0.34.4", "integrity": "sha512-darwin-arm64", "cpu": ["arm64"], "os": ["darwin"], "optional": true,
      "optionalDependencies": {"@img/sharp-libvips-darwin-arm64": "1.2.3"}},
    "node_modules/@img/sharp-libvips-darwin-arm64": {"version": "1.2.3", "integrity": "sha512-libvips-darwin-arm64", "cpu": ["arm64"], "os": ["darwin"], "optional": true},
    "node_modules/@img/sharp-darwin-x64": {"version": "0.34.4", "integrity": "sha512-darwin-x64", "cpu": ["x64"], "os": ["darwin"], "optional": true},
    "node_modules/@img/sharp-linux-x64": {"version": "0.34.4", "integrity": "sha512-linux-x64", "cpu": ["x64"], "os": ["linux"], "libc": ["glibc"], "optional": true},
    "node_modules/@img/sharp-linuxmusl-x64": {"version": "0.34.4", "integrity": "sha512-linuxmusl-x64", "cpu": ["x64"], "os": ["linux"], "libc": ["musl"], "optional": true},
    "node_modules/@img/sharp-win32-x64": {"version": "0.34.4", "integrity": "sha512-win32-x64", "cpu": ["x64"], "os": "win32", "optional": true},
    "node_modules/cli": {"version": "1.0.0", "integrity": "sha512-cli", "dependencies": {"unix-socket": "1.0.0"}},
    "node_modules/unix-socket": {"version": "1.0.0", "integrity": "sha512-unix-socket", "os": ["!win32"]}
  }
}`

func TestPackagesForAPlatformAreInstalledOnItOnly(t *testing.T) {
	both := []string{"node_modules/cli", "node_modules/detect-libc", "node_modules/sharp"}
	for _, tc := range []struct {
		goos, goarch string
		builds       []string
	}{
		{"darwin", "arm64", []string{"node_modules/@img/sharp-darwin-arm64", "node_modules/@img/sharp-libvips-darwin-arm64", "node_modules/unix-socket"}},
		{"darwin", "amd64", []string{"node_modules/@img/sharp-darwin-x64", "node_modules/unix-socket"}},
		{"linux", "amd64", []string{"node_modules/@img/sharp-linux-x64", "node_modules/unix-socket"}},
		{"linux", "arm64", []string{"node_modules/unix-socket"}},
	} {
		pkgs, err := Packages(InstallOptions{Lock: []byte(sharpLock), GOOS: tc.goos, GOARCH: tc.goarch})
		if err != nil {
			t.Fatalf("%s/%s: %v", tc.goos, tc.goarch, err)
		}
		var got []string
		for _, p := range pkgs {
			got = append(got, p.Path)
		}
		want := slices.Sorted(slices.Values(append(slices.Clone(both), tc.builds...)))
		if !slices.Equal(got, want) {
			t.Errorf("%s/%s:\n%s\nwant:\n%s", tc.goos, tc.goarch, strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	// On Windows, unix-socket is needed, not optional, and is not for it.
	_, err := Packages(InstallOptions{Lock: []byte(sharpLock), GOOS: "windows", GOARCH: "amd64"})
	if err == nil || err.Error() != "the lockfile's node_modules/unix-socket is for os !win32 only, not for win32 x64, and is not optional" {
		t.Errorf("windows: %v", err)
	}
}

// Only the packages for the platform are downloaded.
func TestInstallDownloadsThePlatformsBuildsOnly(t *testing.T) {
	files := map[string][]byte{}
	lock := `{"name": "labels", "lockfileVersion": 3, "packages": {
    "": {"name": "labels", "dependencies": {"sharp": "0.34.4"}},
    "node_modules/sharp": {"version": "0.34.4", "integrity": "%s", "optionalDependencies": {"@img/sharp-darwin-arm64": "0.34.4", "@img/sharp-linux-x64": "0.34.4"}},
    "node_modules/@img/sharp-darwin-arm64": {"version": "0.34.4", "integrity": "%s", "cpu": ["arm64"], "os": ["darwin"], "optional": true},
    "node_modules/@img/sharp-linux-x64": {"version": "0.34.4", "integrity": "sha512-not-downloaded", "cpu": ["x64"], "os": ["linux"], "libc": ["glibc"], "optional": true}
  }}`
	files["/sharp/-/sharp-0.34.4.tgz"] = tarball(t, "package/", map[string]string{"lib/index.js": "sharp"})
	files["/@img/sharp-darwin-arm64/-/sharp-darwin-arm64-0.34.4.tgz"] = tarball(t, "package/", map[string]string{"lib/sharp.node": "mach-o"})
	reg := newRegistry(t, files)
	o := InstallOptions{
		Lock:     fmt.Appendf(nil, lock, Integrity(files["/sharp/-/sharp-0.34.4.tgz"]), Integrity(files["/@img/sharp-darwin-arm64/-/sharp-darwin-arm64-0.34.4.tgz"])),
		Dir:      filepath.Join(t.TempDir(), "labels"),
		Registry: reg.URL, GOOS: "darwin", GOARCH: "arm64",
	}
	dir, err := Install(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if n := reg.downloads.Load(); n != 2 {
		t.Errorf("%d downloads, want 2", n)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "node_modules", "@img", "sharp-darwin-arm64", "lib", "sharp.node")); err != nil || string(b) != "mach-o" {
		t.Errorf("%q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "@img", "sharp-linux-x64")); err == nil {
		t.Error("the Linux build is installed on macOS")
	}
}

func TestPlatformsAreNamedAsNodeNamesThem(t *testing.T) {
	for _, tc := range []struct {
		goos, goarch string
		want         platform
	}{
		{"darwin", "arm64", platform{"darwin", "arm64", ""}},
		{"darwin", "amd64", platform{"darwin", "x64", ""}},
		{"linux", "amd64", platform{"linux", "x64", "glibc"}},
		{"linux", "arm64", platform{"linux", "arm64", "glibc"}},
		{"windows", "amd64", platform{"win32", "x64", ""}},
		{"windows", "386", platform{"win32", "ia32", ""}},
		{"illumos", "amd64", platform{"sunos", "x64", ""}},
	} {
		if got := platformOf(tc.goos, tc.goarch); got != tc.want {
			t.Errorf("%s/%s: %+v, want %+v", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

func TestPackagesSayWhichPlatformsTheyAreFor(t *testing.T) {
	for _, tc := range []struct {
		list  names
		value string
		want  bool
	}{
		{nil, "linux", true},
		{names{"any"}, "linux", true},
		{names{"darwin"}, "darwin", true},
		{names{"darwin"}, "linux", false},
		{names{"darwin", "linux"}, "linux", true},
		{names{"!win32"}, "linux", true},
		{names{"!win32"}, "win32", false},
		{names{"!win32", "!darwin"}, "linux", true},
		{names{"!darwin", "linux"}, "linux", true},
		{names{"!darwin", "linux"}, "win32", false},
	} {
		if got := allows(tc.list, tc.value); got != tc.want {
			t.Errorf("%q allows %s: %v", tc.list, tc.value, got)
		}
	}
	// A libc is Linux's only.
	e := lockEntry{OS: names{"linux"}, Libc: names{"glibc"}}
	if !e.fits(platformOf("linux", "arm64")) || e.fits(platformOf("darwin", "arm64")) {
		t.Error("glibc")
	}
	if (lockEntry{Libc: names{"musl"}}).fits(platformOf("linux", "amd64")) {
		t.Error("musl on glibc")
	}
}

// A package a driver bundles with a flaw is removed from the driver once it is
// unpacked: Node.js then finds the fixed version the lockfile has higher up.
func TestAFlawedBundledPackageIsReplaced(t *testing.T) {
	driver := tarball(t, "package/", map[string]string{
		"package.json":                     `{"name":"driver","bundleDependencies":["morgan"]}`,
		"node_modules/morgan/package.json": `{"name":"morgan","version":"1.11.0"}`,
		"node_modules/css/package.json":    `{"name":"css","version":"1.0.7"}`,
	})
	morgan := tarball(t, "package/", map[string]string{"package.json": `{"name":"morgan","version":"1.12.1"}`})
	reg := newRegistry(t, map[string][]byte{"/driver/-/driver-8.7.0.tgz": driver, "/morgan/-/morgan-1.12.1.tgz": morgan})
	lock := fmt.Sprintf(`{"lockfileVersion": 3, "packages": {
		"": {"dependencies": {"driver": "8.7.0", "morgan": "1.12.1"}},
		"node_modules/driver": {"version": "8.7.0", "integrity": %q, "dependencies": {"morgan": "1.11.0", "css": "1.0.7"}},
		"node_modules/driver/node_modules/css": {"version": "1.0.7", "inBundle": true},
		"node_modules/morgan": {"version": "1.12.1", "integrity": %q}
	}}`, Integrity(driver), Integrity(morgan))
	unbundle := []string{"node_modules/driver/node_modules/morgan"}
	dir, err := Install(context.Background(), InstallOptions{Lock: []byte(lock), CacheDir: t.TempDir(), Name: "appium", Registry: reg.URL, Unbundle: unbundle})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "driver", "node_modules", "morgan")); !os.IsNotExist(err) {
		t.Errorf("the flawed morgan is still bundled: %v", err)
	}
	for _, kept := range []string{"node_modules/driver/node_modules/css/package.json", "node_modules/morgan/package.json"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(kept))); err != nil {
			t.Errorf("%s: %v", kept, err)
		}
	}
	plain, err := Packages(InstallOptions{Lock: []byte(lock)})
	if err != nil {
		t.Fatal(err)
	}
	if installName("appium", plain, nil) == filepath.Base(dir) {
		t.Error("an install that unbundles a package is named apart from one that does not")
	}

	// What cannot be unbundled.
	for u, want := range map[string]string{
		"node_modules/morgan":                    "node_modules/morgan is no package another bundles",
		"node_modules/driver/node_modules/css":   "the lockfile still lists node_modules/driver/node_modules/css, which is unbundled",
		"node_modules/driver/node_modules/chalk": "the lockfile has no chalk that Node.js finds in place of node_modules/driver/node_modules/chalk",
	} {
		if _, err := Packages(InstallOptions{Lock: []byte(lock), Unbundle: []string{u}}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %s", u, err, want)
		}
	}
	// A driver that no longer bundles it: the unbundling is out of date.
	lean := tarball(t, "package/", map[string]string{"package.json": `{"name":"driver"}`, "node_modules/css/package.json": `{"name":"css"}`})
	reg2 := newRegistry(t, map[string][]byte{"/driver/-/driver-8.7.0.tgz": lean, "/morgan/-/morgan-1.12.1.tgz": morgan})
	lock2 := strings.Replace(lock, Integrity(driver), Integrity(lean), 1)
	_, err = Install(context.Background(), InstallOptions{Lock: []byte(lock2), CacheDir: t.TempDir(), Name: "appium", Registry: reg2.URL, Unbundle: unbundle})
	if err == nil || !strings.Contains(err.Error(), "is not bundled any more") {
		t.Errorf("an out-of-date unbundling: %v", err)
	}
}
