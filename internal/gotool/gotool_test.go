package gotool

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/mod/sumdb"
	"golang.org/x/mod/sumdb/dirhash"
	"golang.org/x/mod/sumdb/note"
)

const testGo = "go9.9.9"

// goRelease is golang.org/toolchain's zip of a Go release: files under its
// module@version/ prefix, as Go's own distpack writes them.
func goRelease(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(toolchainModule + "@" + toolchainVersion(testGo) + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func hashOf(t *testing.T, archive []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "go.zip")
	if err := os.WriteFile(p, archive, 0o600); err != nil {
		t.Fatal(err)
	}
	h, err := dirhash.HashZip(p, dirhash.Hash1)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// goProxy is a Go module proxy that has one Go release and serves a checksum
// database, sum.test, that lists listed as its hash. GOPROXY and GOSUMDB
// point at it, and nothing of the machine's Go configuration applies.
type goProxy struct {
	*httptest.Server
	downloads, sumdbReads atomic.Int32
	auth                  atomic.Value // the last Authorization header
}

func newGoProxy(t *testing.T, archive []byte, listed string) *goProxy {
	t.Helper()
	skey, vkey, err := note.GenerateKey(rand.Reader, "sum.test")
	if err != nil {
		t.Fatal(err)
	}
	db := sumdb.NewServer(sumdb.NewTestServer(skey, func(path, vers string) ([]byte, error) {
		return fmt.Appendf(nil, "%s %s %s\n%s %s/go.mod h1:x=\n", path, vers, listed, path, vers), nil
	}))
	p := &goProxy{}
	p.auth.Store("")
	zipPath := "/" + toolchainModule + "/@v/" + toolchainVersion(testGo) + ".zip"
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.auth.Store(r.Header.Get("Authorization"))
		switch {
		case r.URL.Path == zipPath:
			p.downloads.Add(1)
			_, _ = w.Write(archive)
		case r.URL.Path == "/sumdb/sum.test/supported":
		case strings.HasPrefix(r.URL.Path, "/sumdb/sum.test/"):
			p.sumdbReads.Add(1)
			http.StripPrefix("/sumdb/sum.test", db).ServeHTTP(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(p.Close)
	isolate(t)
	t.Setenv("GOPROXY", p.URL)
	t.Setenv("GOSUMDB", vkey)
	return p
}

// isolate keeps the machine's Go settings and credentials out of a test.
func isolate(t *testing.T) {
	t.Helper()
	for _, name := range append(netSettings, "AXX_GO") {
		t.Setenv(name, "")
	}
	t.Setenv("GOENV", "off")
	t.Setenv("NETRC", filepath.Join(t.TempDir(), "none"))
}

func goFiles() map[string]string {
	return map[string]string{
		"bin/" + goExe():            "#!/bin/sh\necho " + testGo + "\n",
		"pkg/tool/linux_amd64/link": "link",
		"lib/wasm/go_js_wasm_exec":  "#!/bin/sh\n",
		"lib/time/zoneinfo.zip":     "zones",
		"src/_go.mod":               "module std\n",
		"src/cmd/_go.mod":           "module cmd\n",
		"README.md":                 "Go",
	}
}

func TestDownloadsGoOnceFromTheModuleProxy(t *testing.T) {
	archive := goRelease(t, goFiles())
	proxy := newGoProxy(t, archive, hashOf(t, archive))
	// Another Go's tree in the caller's environment is not the toolchain's.
	t.Setenv("GOROOT", "/opt/go1.0.0")
	t.Setenv("GOTOOLDIR", "/opt/go1.0.0/pkg/tool")
	cache := t.TempDir()
	var log strings.Builder
	o := Options{CacheDir: cache, Version: testGo, Log: &log}
	tc, err := Ensure(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(cache, fmt.Sprintf("%s.%s-%s", testGo, runtime.GOOS, runtime.GOARCH), "go")
	if want := filepath.Join(root, "bin", goExe()); tc.Go != want {
		t.Errorf("go is %s, want %s", tc.Go, want)
	}
	env := strings.Join(tc.Env, "\n")
	for _, v := range []string{"GOTOOLCHAIN=local", "CGO_ENABLED=0", "GOTELEMETRY=off", "GOENV=off", "GOCACHE=" + filepath.Join(cache, "build"), "GOMODCACHE="} {
		if !strings.Contains(env, v) {
			t.Errorf("the environment lacks %s", v)
		}
	}
	if strings.Contains(env, "GOROOT=") || strings.Contains(env, "GOTOOLDIR=") {
		t.Errorf("the toolchain inherits another Go's tree:\n%s", env)
	}
	if !strings.Contains(log.String(), "setting up") || strings.Contains(strings.ToLower(log.String()), "go ") {
		t.Errorf("what the user sees: %q", log.String())
	}

	// Laid out as the go command lays out a Go it downloads.
	for name, want := range map[string]string{"src/go.mod": "module std\n", "src/_go.mod": "module std\n", "src/cmd/go.mod": "module cmd\n", "README.md": "Go"} {
		if b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))); err != nil || string(b) != want {
			t.Errorf("%s: %q %v", name, b, err)
		}
	}
	if runtime.GOOS != "windows" {
		for name, exec := range map[string]bool{"bin/" + goExe(): true, "pkg/tool/linux_amd64/link": true, "lib/wasm/go_js_wasm_exec": true, "lib/time/zoneinfo.zip": false, "README.md": false} {
			st, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil || (st.Mode()&0o111 != 0) != exec {
				t.Errorf("%s: %v %v, want executable %v", name, st.Mode(), err, exec)
			}
		}
	}
	if proxy.sumdbReads.Load() == 0 {
		t.Error("the download was not checked against the checksum database")
	}

	// The second time, it is there.
	if _, err := Ensure(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if n := proxy.downloads.Load(); n != 1 {
		t.Errorf("%d downloads", n)
	}
}

func TestAGoTheChecksumDatabaseDoesNotListIsRefused(t *testing.T) {
	archive := goRelease(t, goFiles())
	newGoProxy(t, archive, hashOf(t, goRelease(t, map[string]string{"bin/go": "another"})))
	cache := t.TempDir()
	_, err := managed(context.Background(), Options{CacheDir: cache, Version: testGo, Log: &strings.Builder{}})
	if err == nil || !strings.Contains(err.Error(), "checksum database sum.test lists") {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(filepath.Join(cache, fmt.Sprintf("%s.%s-%s", testGo, runtime.GOOS, runtime.GOARCH))); err == nil {
		t.Error("the refused Go is in the cache")
	}
}

// As the go command, axx checks every Go it downloads against the checksum
// database: GOSUMDB=off, GONOSUMDB and GOPRIVATE do not turn that off.
func TestTheChecksumDatabaseAlwaysChecksGo(t *testing.T) {
	archive := goRelease(t, goFiles())
	newGoProxy(t, archive, hashOf(t, goRelease(t, map[string]string{"bin/go": "another"})))
	t.Setenv("GONOSUMDB", "golang.org")
	t.Setenv("GOPRIVATE", "golang.org/x")
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err == nil || !strings.Contains(err.Error(), "checksum database") {
		t.Errorf("GONOSUMDB: %v", err)
	}
	t.Setenv("GOSUMDB", "off")
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err == nil || !strings.Contains(err.Error(), "GOSUMDB=off") {
		t.Errorf("GOSUMDB=off: %v", err)
	}
}

func TestGOPROXYAsTheGoCommandReadsIt(t *testing.T) {
	archive := goRelease(t, goFiles())
	proxy := newGoProxy(t, archive, hashOf(t, archive))
	empty := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(empty.Close)
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "down", http.StatusBadGateway) }))
	t.Cleanup(broken.Close)
	for goproxy, want := range map[string]string{
		empty.URL + "," + proxy.URL:  "",              // one without it: the next
		broken.URL + "|" + proxy.URL: "",              // | moves on whatever failed
		broken.URL + "," + proxy.URL: "502",           // , does not
		empty.URL + ",off":           "GOPROXY=off",   // off ends the list
		"off":                        "GOPROXY=off",   //
		empty.URL:                    "no proxy",      // nothing has it
		"ftp://example.com":          "not a proxy's", //
		",":                          "lists no proxy",
	} {
		t.Run(goproxy, func(t *testing.T) {
			t.Setenv("GOPROXY", goproxy)
			_, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}})
			if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Errorf("got %v, want %q", err, want)
			}
		})
	}
}

// The network settings `go env -w` wrote apply: to the download, and to the
// builds, which read no Go configuration file.
func TestTheUsersGoConfigurationApplies(t *testing.T) {
	archive := goRelease(t, goFiles())
	proxy := newGoProxy(t, archive, hashOf(t, archive))
	goproxy, gosumdb := os.Getenv("GOPROXY"), os.Getenv("GOSUMDB")
	t.Setenv("GOPROXY", "")
	t.Setenv("GOSUMDB", "")
	file := filepath.Join(t.TempDir(), "env")
	content := "GOPROXY=" + goproxy + "\nGOSUMDB=" + gosumdb + "\nGOPRIVATE=example.com/private\nGOFLAGS=-tags=nothing\n# a comment\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", file)
	t.Setenv("GOINSECURE", "example.com/insecure")
	tc, err := Ensure(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo})
	if err != nil || proxy.downloads.Load() != 1 {
		t.Fatalf("%v, %d downloads", err, proxy.downloads.Load())
	}
	env := strings.Join(tc.Env, "\n") + "\n"
	for _, v := range []string{"GOPROXY=" + goproxy + "\n", "GOSUMDB=" + gosumdb + "\n", "GOPRIVATE=example.com/private\n", "GOINSECURE=example.com/insecure\n"} {
		if !strings.Contains(env, v) {
			t.Errorf("the builds lack %s", v)
		}
	}
	if strings.Contains(env, "-tags=nothing") {
		t.Error("the builds take the file's GOFLAGS, which is not a network setting")
	}
}

// A proxy that needs credentials gets them, from its address or from
// .netrc as the go command sends them, and no message shows them.
func TestCredentials(t *testing.T) {
	archive := goRelease(t, goFiles())
	proxy := newGoProxy(t, archive, hashOf(t, archive))

	t.Setenv("GOPROXY", strings.Replace(proxy.URL, "http://", "http://ci:secret@", 1))
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err != nil {
		t.Fatal(err)
	}
	if got := proxy.auth.Load().(string); !strings.HasPrefix(got, "Basic ") {
		t.Errorf("the proxy got %q", got)
	}
	t.Setenv("GOPROXY", strings.Replace(proxy.URL, "http://", "http://ci:secret@", 1)+"/missing")
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("a failure shows the credentials: %v", err)
	}

	// .netrc's credentials go only over HTTPS.
	tls := httptest.NewTLSServer(proxy.Config.Handler)
	t.Cleanup(tls.Close)
	defer func(c *http.Client) { client = c }(client)
	client = tls.Client()
	netrc := filepath.Join(t.TempDir(), "netrc")
	host := strings.TrimPrefix(tls.URL, "https://")
	if err := os.WriteFile(netrc, []byte("machine other.example.com login x password y\nmachine "+host+"\n  login ci\n  password secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETRC", netrc)
	t.Setenv("GOPROXY", tls.URL)
	proxy.auth.Store("")
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err != nil {
		t.Fatal(err)
	}
	if got := proxy.auth.Load().(string); got != "Basic Y2k6c2VjcmV0" {
		t.Errorf(".netrc: the proxy got %q", got)
	}
	t.Setenv("GOAUTH", "off")
	proxy.auth.Store("")
	if _, err := managed(context.Background(), Options{CacheDir: t.TempDir(), Version: testGo, Log: &strings.Builder{}}); err != nil || proxy.auth.Load().(string) != "" {
		t.Errorf("GOAUTH=off: %v, the proxy got %q", err, proxy.auth.Load())
	}
}

func TestArchiveEntriesStayInTheirFolder(t *testing.T) {
	for name, entries := range map[string][]zip.FileHeader{
		"outside the module":   {{Name: "golang.org/other@v1.0.0/bin/go"}},
		"leaves the directory": {{Name: toolchainModule + "@" + toolchainVersion(testGo) + "/../../escape"}},
		"a backslash":          {{Name: toolchainModule + "@" + toolchainVersion(testGo) + `/..\..\escape`}},
		"a link":               {link(toolchainModule + "@" + toolchainVersion(testGo) + "/lib/go")},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for _, h := range entries {
				w, err := zw.CreateHeader(&h)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = w.Write([]byte("x"))
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(t.TempDir(), "go.zip")
			if err := os.WriteFile(archive, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := unpack(archive, toolchainVersion(testGo), t.TempDir()); err == nil {
				t.Error("unpacked")
			}
		})
	}
}

func link(name string) zip.FileHeader {
	h := zip.FileHeader{Name: name}
	h.SetMode(os.ModeSymlink | 0o777)
	return h
}

func TestParseNetrc(t *testing.T) {
	got := parseNetrc("machine a.example.com login u1 password p1\n" +
		"macdef init\nmachine b.example.com login no password no\n\n" +
		"machine c.example.com\n login u3\n password p3\n" +
		"default\nmachine e.example.com login no password no\n")
	want := []netrcMachine{{"a.example.com", "u1", "p1"}, {"c.example.com", "u3", "p3"}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestAXXGoWins(t *testing.T) {
	t.Setenv("AXX_GO", "/opt/go/bin/go")
	tc, err := Ensure(context.Background(), Options{CacheDir: t.TempDir()})
	if err != nil || tc.Go != "/opt/go/bin/go" {
		t.Fatalf("%+v %v", tc, err)
	}
}
