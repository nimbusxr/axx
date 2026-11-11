package docsite

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The Accept headers of docs/cloudfront/router_test.mjs.
const (
	browser  = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
	opencode = "text/markdown;q=1.0, text/x-markdown;q=0.9, text/plain;q=0.8, text/html;q=0.7, */*;q=0.1"
	claude   = "text/markdown, text/html, */*"
)

// testSite is a built site, as the docs' build writes one: pages with their
// Markdown twins, a page that moved, assets and the 404 page.
var testSite = map[string]string{
	"index.html":                          "<h1>home</h1>",
	"index.md":                            "# home",
	"404.html":                            "<h1>not here</h1>",
	"guides/install/index.html":           "<h1>install</h1>",
	"guides/install.md":                   "# install",
	"references/config/index.html":        "<h1>config</h1>",
	"references/configuration/index.html": `<!doctype html><title>Redirecting to: /references/config/</title><meta http-equiv="refresh" content="0;url=/references/config/">`,
	"references/elsewhere/index.html":     `<meta http-equiv="refresh" content="0;url=https://example.com/">`,
	"_astro/x.css":                        "body{}",
	"pagefind/pagefind.js":                "export {}",
	"llms.txt":                            "# axx",
	".well-known/agent-skills/index.json": "{}",
}

func writeSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range testSite {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestHandlerAnswersAsTheSite(t *testing.T) {
	h := Handler(writeSite(t))
	for _, c := range []struct {
		uri, accept, want string
	}{
		{"/", browser, "200 <h1>home</h1>"},
		{"/", opencode, "200 # home"},
		{"/guides/install/", claude, "200 # install"},
		{"/guides/install/", "text/html, text/markdown", "200 <h1>install</h1>"},
		{"/guides/install/", "", "200 <h1>install</h1>"},
		{"/_astro/x.css", "text/markdown", "200 body{}"},
		{"/guides/install.md", "", "200 # install"},
		{"/guides/install", "", "301 /guides/install/"},
		{"/guides/install?q=1", "", "301 /guides/install/?q=1"},
		{"/references/configuration/", claude, "301 /references/config/"},
		{"/references/configuration", "", "301 /references/config/"},
		{"/references/elsewhere/", "", "200 " + testSite["references/elsewhere/index.html"]},
		{"/guides/../guides/install/", "", "301 /guides/install/"},
		{"/no/such/page/", browser, "404 <h1>not here</h1>"},
		{"/no/such/page/", claude, "404 <h1>not here</h1>"},
		{"/no-such-file.js", "", "404 <h1>not here</h1>"},
		{"/llms.txt", "", "200 # axx"},
		{"/.well-known/agent-skills/index.json", "", "200 {}"},
		{"/pagefind/pagefind.js", "", "200 export {}"},
	} {
		req := httptest.NewRequest(http.MethodGet, c.uri, nil)
		if c.accept != "" {
			req.Header.Set("Accept", c.accept)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		got := rec.Code
		body := rec.Body.String()
		if got == http.StatusMovedPermanently {
			body = rec.Header().Get("Location")
		}
		if have := strconv.Itoa(got) + " " + body; have != c.want {
			t.Errorf("%s [%s]: %s, want %s", c.uri, c.accept, have, c.want)
		}
	}
}

func TestHandlerTypes(t *testing.T) {
	h := Handler(writeSite(t))
	for uri, want := range map[string]string{
		"/guides/install.md": "text/markdown; charset=utf-8",
		"/guides/install/":   "text/html; charset=utf-8",
		"/_astro/x.css":      "text/css; charset=utf-8",
		"/no/such/page/":     "text/html; charset=utf-8",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, uri, nil))
		if got := rec.Header().Get("Content-Type"); got != want {
			t.Errorf("%s: Content-Type %q, want %q", uri, got, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/guides/install/", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d with %d bytes", rec.Code, rec.Body.Len())
	}
}

func TestPrefersMarkdown(t *testing.T) {
	for accept, want := range map[string]bool{
		"":                                   false,
		browser:                              false,
		opencode:                             true,
		claude:                               true,
		"text/markdown":                      true,
		"text/html, text/markdown":           false,
		"text/markdown;q=0":                  false,
		"text/markdown;q=0.5, text/html":     false,
		"text/html;q=0.5, text/x-markdown":   true,
		"TEXT/MARKDOWN":                      true,
		"text/markdown;q=x, text/html;q=0.1": false,
	} {
		if got := PrefersMarkdown(accept); got != want {
			t.Errorf("PrefersMarkdown(%q) = %v, want %v", accept, got, want)
		}
	}
}

type entry struct {
	name, content string
	typ           byte
}

func archive(t *testing.T, entries ...entry) string {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Mode: 0o644, Typeflag: e.typ, Size: int64(len(e.content))}
		if e.typ == tar.TypeDir {
			hdr.Mode, hdr.Size = 0o755, 0
		}
		if e.typ == tar.TypeSymlink {
			hdr.Linkname, hdr.Size = e.content, 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typ == tar.TypeReg {
			if _, err := tw.Write([]byte(e.content)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "axx_0.7.0_docs.tar.gz")
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpack(t *testing.T) {
	// As tar -C dist -czf archive . writes it: entries below ./.
	a := archive(t,
		entry{"./", "", tar.TypeDir},
		entry{"./index.html", "<h1>home</h1>", tar.TypeReg},
		entry{"./guides/", "", tar.TypeDir},
		entry{"./guides/install/index.html", "<h1>install</h1>", tar.TypeReg},
		entry{"_astro/x.css", "body{}", tar.TypeReg},
	)
	dir := filepath.Join(t.TempDir(), "site")
	if err := Unpack(a, dir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"index.html": "<h1>home</h1>", "guides/install/index.html": "<h1>install</h1>", "_astro/x.css": "body{}"} {
		if b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name))); err != nil || string(b) != want {
			t.Errorf("%s: %q, %v", name, b, err)
		}
	}
	if !IsSite(dir) {
		t.Error("not a site")
	}
}

func TestUnpackRefuses(t *testing.T) {
	for name, a := range map[string]string{
		"an entry out of the site": archive(t, entry{"index.html", "x", tar.TypeReg}, entry{"../evil", "x", tar.TypeReg}),
		"an absolute entry":        archive(t, entry{"index.html", "x", tar.TypeReg}, entry{"/etc/evil", "x", tar.TypeReg}),
		"a link":                   archive(t, entry{"index.html", "x", tar.TypeReg}, entry{"link", "/etc/passwd", tar.TypeSymlink}),
		"no index.html":            archive(t, entry{"guides/index.html", "x", tar.TypeReg}),
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "site")
			err := Unpack(a, dir)
			if err == nil {
				t.Fatal("unpacked")
			}
			if name == "no index.html" && !errors.Is(err, ErrNotASite) {
				t.Fatalf("err: %v", err)
			}
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); statErr == nil {
				t.Fatal("wrote outside the site")
			}
		})
	}
	notGzip := filepath.Join(t.TempDir(), "site.tar.gz")
	if err := os.WriteFile(notGzip, []byte("not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Unpack(notGzip, filepath.Join(t.TempDir(), "site")); err == nil {
		t.Fatal("unpacked a file that is not a .tar.gz")
	}
}

// fetcher hands out an archive, counting what it was asked for.
type fetcher struct {
	archive string
	err     error
	tags    []string
}

func (f *fetcher) FetchDocs(_ context.Context, tag, dir string) (string, error) {
	f.tags = append(f.tags, tag)
	if f.err != nil {
		return "", f.err
	}
	b, err := os.ReadFile(f.archive)
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, filepath.Base(f.archive))
	return p, os.WriteFile(p, b, 0o644)
}

func TestReleaseIsDownloadedOnceIntoTheCache(t *testing.T) {
	f := &fetcher{archive: archive(t, entry{"index.html", "<h1>0.7.0</h1>", tar.TypeReg})}
	cache := filepath.Join(t.TempDir(), "docs")
	for range 2 {
		dir, err := Release(context.Background(), f, "0.7.0", cache)
		if err != nil {
			t.Fatal(err)
		}
		if dir != filepath.Join(cache, "0.7.0") || !IsSite(dir) {
			t.Fatalf("dir %s", dir)
		}
	}
	if len(f.tags) != 1 || f.tags[0] != "v0.7.0" {
		t.Fatalf("fetched %v, want v0.7.0 once", f.tags)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 1 {
		t.Fatalf("the cache holds %d entries: what it unpacked beside the site stays", len(entries))
	}

	f = &fetcher{err: errors.New("offline")}
	if _, err := Release(context.Background(), f, "0.8.0", cache); err == nil {
		t.Fatal("no error")
	}
	if _, err := os.Stat(filepath.Join(cache, "0.8.0")); err == nil {
		t.Fatal("a failed download left a folder")
	}
	f = &fetcher{archive: archive(t, entry{"guides/index.html", "x", tar.TypeReg})}
	if _, err := Release(context.Background(), f, "0.9.0", cache); !errors.Is(err, ErrNotASite) {
		t.Fatalf("an archive of no site: %v", err)
	}
}

func TestOpenAFolderOrAnArchive(t *testing.T) {
	site := writeSite(t)
	dir, cleanup, err := Open(site)
	if err != nil || dir != site {
		t.Fatalf("folder: %s, %v", dir, err)
	}
	cleanup()
	if !IsSite(site) {
		t.Fatal("cleanup removed a folder the person gave")
	}

	dir, cleanup, err = Open(archive(t, entry{"index.html", "<h1>home</h1>", tar.TypeReg}))
	if err != nil || !IsSite(dir) {
		t.Fatalf("archive: %s, %v", dir, err)
	}
	cleanup()
	if _, err := os.Stat(dir); err == nil {
		t.Fatal("cleanup left the unpacked archive")
	}

	if _, _, err := Open(t.TempDir()); !errors.Is(err, ErrNotASite) {
		t.Fatalf("an empty folder: %v", err)
	}
}
