package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func release(tag string, assets ...string) Release {
	r := Release{Tag: tag}
	for _, a := range assets {
		r.Assets = append(r.Assets, struct {
			Name string `json:"name"`
		}{a})
	}
	return r
}

// A release is published before its build uploads its files, so the newest one
// can have none yet.
func TestNewestHasItsFilesUp(t *testing.T) {
	releases := []Release{
		release("v0.5.0"),
		release("v0.4.0", ChecksumsFile, "axx_0.4.0_linux_amd64.tar.gz"),
		release("v0.3.0", ChecksumsFile, "axx_0.3.0_linux_amd64.tar.gz", "axx_0.3.0_darwin_arm64.tar.gz"),
	}
	if r := Newest(releases, "linux", "amd64"); r == nil || r.Tag != "v0.4.0" {
		t.Errorf("linux: %+v", r)
	}
	if r := Newest(releases, "darwin", "arm64"); r == nil || r.Tag != "v0.3.0" {
		t.Errorf("darwin: %+v", r)
	}
	if r := Newest(releases, "windows", "arm64"); r != nil {
		t.Errorf("windows: %+v", r)
	}
}

func TestReleasesAreTheCLIsPublishedOnes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"tag_name":"v0.4.0"},{"tag_name":"intellij-v0.2.2"},{"tag_name":"wiremock-openapi-v0.1.0"},{"tag_name":"nightly"},{"tag_name":"v0.5.0","draft":true},{"tag_name":"v0.3.1"}]`))
	}))
	defer srv.Close()
	c := &Client{API: srv.URL, HTTP: srv.Client()}
	got, err := c.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, r := range got {
		tags = append(tags, r.Tag)
	}
	if want := []string{"v0.4.0", "v0.3.1"}; !reflect.DeepEqual(tags, want) {
		t.Errorf("tags %v, want %v", tags, want)
	}
}

func TestBetweenIsAfterFromUpToTo(t *testing.T) {
	releases := []Release{release("v0.10.0"), release("v0.9.0"), release("v0.4.0"), release("v0.3.1")}
	var got []string
	for _, r := range Between(releases, "0.3.1", "0.9.0") {
		got = append(got, r.Version())
	}
	if want := []string{"0.9.0", "0.4.0"}; !reflect.DeepEqual(got, want) {
		t.Errorf("%v, want %v (0.10.0 is newer than 0.9.0, by number)", got, want)
	}
}

// The notes as release-please writes them: v0.4.0's.
func TestNotesLeaveOutTheLinks(t *testing.T) {
	body := "## [0.4.0](https://github.com/nimbusxr/axx/compare/v0.3.1...v0.4.0) (2026-11-02)\n\n\n" +
		"### ⚠ BREAKING CHANGES\n\n" +
		"* **sql:** the database's clock in seeds ([#292](https://github.com/nimbusxr/axx/issues/292))\n\n" +
		"### Features\n\n" +
		"* **sql:** triggers before updates ([#293](https://github.com/nimbusxr/axx/issues/293)) ([042359d](https://github.com/nimbusxr/axx/commit/042359d))\n" +
		"* a step without a scope ([#1](https://github.com/nimbusxr/axx/issues/1))\n"
	want := []Section{
		{Title: "Breaking changes", Items: []string{"sql: the database's clock in seeds"}},
		{Title: "Features", Items: []string{"sql: triggers before updates", "a step without a scope"}},
	}
	if got := Notes(body); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func tgz(t *testing.T, name, content string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, content string }{{"LICENSE", "license"}, {name, content}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// files serves a release's files, and records what authorization each request had.
func files(t *testing.T, seen map[string]string, content map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen[r.URL.Path] = r.Header.Get("Authorization")
		b, ok := content[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchChecksAndUnpacks(t *testing.T) {
	archive := tgz(t, "axx", "the binary")
	name := "axx_0.4.0_linux_amd64.tar.gz"
	seen := map[string]string{}
	srv := files(t, seen, map[string][]byte{
		"/v0.4.0/checksums.txt": []byte(sum(archive) + "  " + name + "\n"),
		"/v0.4.0/" + name:       archive,
	})
	dir := t.TempDir()
	c := &Client{Downloads: srv.URL, Token: "the-token", HTTP: srv.Client()}
	got, err := c.Fetch(context.Background(), "v0.4.0", "linux", "amd64", dir)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(got.Binary)
	if string(b) != "the binary" {
		t.Errorf("binary: %q", b)
	}
	if fi, _ := os.Stat(got.Binary); runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("not executable: %v", fi.Mode())
	}
	for path, auth := range seen {
		if auth != "" {
			t.Errorf("%s got the token, which is for the API only", path)
		}
	}
}

func TestFetchRefusesAnArchiveThatIsNotTheChecksummedOne(t *testing.T) {
	name := "axx_0.4.0_linux_amd64.tar.gz"
	srv := files(t, map[string]string{}, map[string][]byte{
		"/v0.4.0/checksums.txt": []byte(sum([]byte("another")) + "  " + name + "\n"),
		"/v0.4.0/" + name:       tgz(t, "axx", "the binary"),
	})
	dir := t.TempDir()
	c := &Client{Downloads: srv.URL, HTTP: srv.Client()}
	_, err := c.Fetch(context.Background(), "v0.4.0", "linux", "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "axx")); statErr == nil {
		t.Error("unpacked an archive that failed its checksum")
	}
}

func TestFetchSaysWhatIsMissing(t *testing.T) {
	srv := files(t, map[string]string{}, map[string][]byte{})
	c := &Client{Downloads: srv.URL, HTTP: srv.Client()}
	if _, err := c.Fetch(context.Background(), "v9.9.9", "linux", "amd64", t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Errorf("err: %v", err)
	}
}

func TestFetchTakesTheNightlysArchiveFromItsChecksums(t *testing.T) {
	archive := tgz(t, "axx", "nightly")
	name := "axx_0.1.0-SNAPSHOT-abc_linux_amd64.tar.gz"
	srv := files(t, map[string]string{}, map[string][]byte{
		"/nightly/checksums.txt": []byte(sum(archive) + "  " + name + "\n"),
		"/nightly/" + name:       archive,
	})
	c := &Client{Downloads: srv.URL, HTTP: srv.Client()}
	got, err := c.Fetch(context.Background(), Nightly, "linux", "amd64", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got.Binary); string(b) != "nightly" {
		t.Errorf("binary: %q", b)
	}
}

func TestFetchUnpacksWindowsZips(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, err := z.Create("axx.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("the exe")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	name := "axx_0.4.0_windows_amd64.zip"
	srv := files(t, map[string]string{}, map[string][]byte{
		"/v0.4.0/checksums.txt": []byte(sum(b.Bytes()) + "  " + name + "\n"),
		"/v0.4.0/" + name:       b.Bytes(),
	})
	c := &Client{Downloads: srv.URL, HTTP: srv.Client()}
	got, err := c.Fetch(context.Background(), "v0.4.0", "windows", "amd64", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got.Binary); string(b) != "the exe" {
		t.Errorf("binary: %q", b)
	}
}

func TestReplacePutsTheNewFileInPlace(t *testing.T) {
	dir := t.TempDir()
	exe, fresh := filepath.Join(dir, "axx"), filepath.Join(dir, "new")
	for p, s := range map[string]string{exe: "old", fresh: "new"} {
		if err := os.WriteFile(p, []byte(s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := Replace(exe, fresh); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("exe: %q", b)
	}
}

func TestHomebrewLinkOutlivesTheVersion(t *testing.T) {
	for exe, want := range map[string]string{
		"/opt/homebrew/Caskroom/axx/0.4.0/axx":              "/opt/homebrew/bin/axx",
		"/home/linuxbrew/.linuxbrew/Caskroom/axx/0.4.0/axx": "/home/linuxbrew/.linuxbrew/bin/axx",
		"/usr/local/Cellar/axx/0.4.0/bin/axx":               "/usr/local/bin/axx",
		"/usr/local/bin/axx":                                "",
	} {
		if got := filepath.ToSlash(HomebrewLink(filepath.FromSlash(exe))); got != want {
			t.Errorf("HomebrewLink(%q) = %q, want %q", exe, got, want)
		}
	}
}

func TestDetectSaysHowAnAxxUpgrades(t *testing.T) {
	for _, c := range []struct {
		exe, stamped, current string
		want                  Method
	}{
		{"/opt/homebrew/Caskroom/axx/0.4.0/axx", "0.4.0", "0.4.0", Homebrew},
		{"/home/linuxbrew/.linuxbrew/Caskroom/axx/0.4.0/axx", "0.4.0", "0.4.0", Homebrew},
		{"/usr/local/bin/axx", "0.4.0", "0.4.0", Archive},
		{"/home/me/.local/bin/axx", "0.4.0", "0.4.0", Archive},
		{"/home/me/go/bin/axx", "", "0.4.0", GoInstall},
		{"/home/me/go/bin/axx", "", "0.4.1-0.20260925184804-3bf0dd25927c", GoInstall},
		{"/src/axx/bin/axx", "", "0.0.0-dev", Source},
		{"/src/axx/bin/axx", "", "0.4.0+dirty", Source},
		{"/usr/local/bin/axx", "0.1.0-SNAPSHOT-abc", "0.1.0-SNAPSHOT-abc", Prerelease},
	} {
		if got := Detect(c.exe, c.stamped, c.current); got != c.want {
			t.Errorf("Detect(%q, %q, %q) = %q, want %q", c.exe, c.stamped, c.current, got, c.want)
		}
	}
}

// holdOpen keeps a file from being moved on Windows, where Go opens files so that
// they can't be, as scanners do, until release is called.
func holdOpen(t *testing.T, path string) (release func()) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("any other OS moves a file that is open")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	release = func() { once.Do(func() { _ = f.Close() }) }
	t.Cleanup(release)
	return release
}

func TestReplaceWaitsOutSomethingHoldingTheFile(t *testing.T) {
	dir := t.TempDir()
	exe, fresh := filepath.Join(dir, "axx.exe"), filepath.Join(dir, "new.exe")
	for path, content := range map[string]string{exe: "old", fresh: "new"} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	release := holdOpen(t, exe)
	time.AfterFunc(300*time.Millisecond, release)
	if err := Replace(exe, fresh); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Errorf("axx.exe holds %q", b)
	}
}

func TestReplaceGivesUpOnSomethingThatKeepsHoldingTheFile(t *testing.T) {
	dir := t.TempDir()
	exe, fresh := filepath.Join(dir, "axx.exe"), filepath.Join(dir, "new.exe")
	for path, content := range map[string]string{exe: "old", fresh: "new"} {
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	holdOpen(t, exe)
	old := moveWait
	moveWait = 200 * time.Millisecond
	t.Cleanup(func() { moveWait = old })
	if err := Replace(exe, fresh); err == nil {
		t.Fatal("it replaced a file that stayed held")
	}
	if b, _ := os.ReadFile(exe); string(b) != "old" {
		t.Errorf("axx.exe holds %q", b)
	}
}
