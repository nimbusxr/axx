// Package docsite serves axx's documentation site on this machine, for `axx
// docs serve`: the site a release publishes beside its archives
// (axx_<version>_docs.tar.gz, in its checksums.txt), unpacked into axx's
// cache, or a built site, or its archive, that a person gives.
//
// It answers as the site does (docs/cloudfront/router.js): a page that moved
// moves for real (a 301), an address without its slash moves to it, a
// request that prefers text/markdown gets the page's Markdown twin, a
// folder's address its index.html, and an address of nothing the site's 404
// page.
package docsite

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	// maxSiteBytes and maxSiteFiles bound what an archive unpacks to.
	maxSiteBytes = 1 << 30
	maxSiteFiles = 100_000
)

// ErrNotASite means a folder or archive holds no site: no index.html at its
// top.
var ErrNotASite = errors.New("it is not a docs site: there is no index.html at its top")

// IsSite reports whether dir holds a built site.
func IsSite(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, "index.html"))
	return err == nil && st.Mode().IsRegular()
}

// Unpack writes the site an archive (a .tar.gz of the site's files) holds to
// dir, which must not exist yet. It writes only regular files and folders
// below dir: an entry that leads out of it, or a link, is refused.
func Unpack(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a .tar.gz: %w", filepath.Base(archive), err)
	}
	defer gz.Close()
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for files := 0; ; files++ {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if files > maxSiteFiles {
			return fmt.Errorf("it holds more than %d files", maxSiteFiles)
		}
		name := path.Clean(strings.TrimPrefix(hdr.Name, "./"))
		if name == "." {
			continue
		}
		if !fs.ValidPath(name) {
			return fmt.Errorf("its entry %q leads out of the site", hdr.Name)
		}
		dst := filepath.Join(dir, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += hdr.Size
			if total > maxSiteBytes {
				return fmt.Errorf("it holds more than %d bytes", maxSiteBytes)
			}
			if err := writeFile(tr, dst, hdr.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("its entry %q is not a file or a folder", hdr.Name)
		}
	}
	if !IsSite(dir) {
		return ErrNotASite
	}
	return nil
}

func writeFile(r io.Reader, dst string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(f, r, size); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Handler serves the site in dir as the docs site answers.
func Handler(dir string) http.Handler {
	return &site{fsys: os.DirFS(dir)}
}

type site struct {
	fsys fs.FS
}

// moved is how Astro writes a page that moved: a page of a meta refresh to
// where it went (docs/scripts/redirects.mjs reads the same).
var moved = regexp.MustCompile(`<meta http-equiv="refresh" content="0;url=([^"]+)"`)

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uri := r.URL.Path
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	clean := path.Clean(uri)
	if clean != "/" && strings.HasSuffix(uri, "/") {
		clean += "/"
	}
	if clean != uri {
		s.redirect(w, r, clean)
		return
	}
	// A page that moved.
	folder := strings.TrimSuffix(uri, "/")
	if to := s.movedTo(strings.TrimPrefix(folder+"/index.html", "/")); to != "" {
		s.redirect(w, r, to)
		return
	}
	// /guides/install is /guides/install/.
	last := uri[strings.LastIndex(uri, "/")+1:]
	if last != "" && !strings.Contains(last, ".") {
		s.redirect(w, r, uri+"/")
		return
	}
	name := strings.TrimPrefix(uri, "/")
	if strings.HasSuffix(uri, "/") {
		if PrefersMarkdown(r.Header.Get("Accept")) {
			// The home has its twin inside it; any other page beside it.
			if uri == "/" {
				name = "index.md"
			} else {
				name = strings.TrimSuffix(name, "/") + ".md"
			}
		} else {
			name += "index.html"
		}
	}
	if !s.isFile(name) {
		s.notFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".md") {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.fsys, name)
}

func (s *site) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.URL.RawQuery != "" && !strings.Contains(to, "?") {
		to += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, to, http.StatusMovedPermanently)
}

func (s *site) isFile(name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	st, err := fs.Stat(s.fsys, name)
	return err == nil && st.Mode().IsRegular()
}

// movedTo is where the page of index, an index.html, went, when it is one
// that moved.
func (s *site) movedTo(index string) string {
	if !s.isFile(index) {
		return ""
	}
	f, err := s.fsys.Open(index)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 2048)
	n, _ := io.ReadFull(f, head)
	if m := moved.FindSubmatch(head[:n]); m != nil {
		to := string(m[1])
		// Only to a page of the site: a moved page never leads elsewhere.
		if strings.HasPrefix(to, "/") && !strings.HasPrefix(to, "//") {
			return to
		}
	}
	return ""
}

func (s *site) notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	page, err := fs.ReadFile(s.fsys, "404.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(page)))
	w.WriteHeader(http.StatusNotFound)
	if r.Method != http.MethodHead {
		_, _ = w.Write(page)
	}
}

// PrefersMarkdown reports whether an Accept header ranks text/markdown above
// text/html, or lists it first at the same rank: what coding agents ask for.
func PrefersMarkdown(accept string) bool {
	if accept == "" {
		return false
	}
	md, html := -1.0, -1.0
	mdAt, htmlAt := -1, -1
	for i, part := range strings.Split(accept, ",") {
		fields := strings.Split(strings.ToLower(strings.TrimSpace(part)), ";")
		typ := strings.TrimSpace(fields[0])
		q := 1.0
		for _, p := range fields[1:] {
			if k, v, ok := strings.Cut(strings.TrimSpace(p), "="); ok && k == "q" {
				f, err := strconv.ParseFloat(v, 64)
				if err != nil {
					f = -1 // a rank it cannot read: as the router, which reads NaN, it never wins
				}
				q = f
			}
		}
		if (typ == "text/markdown" || typ == "text/x-markdown") && q > md {
			md, mdAt = q, i
		}
		if typ == "text/html" && q > html {
			html, htmlAt = q, i
		}
	}
	if md <= 0 {
		return false
	}
	if html < 0 {
		return true
	}
	return md > html || (md == html && mdAt < htmlAt)
}

// Fetcher downloads the archive of a release's docs into a folder, checked
// against the release's checksums: selfupdate.Client.FetchDocs.
type Fetcher interface {
	FetchDocs(ctx context.Context, tag, dir string) (string, error)
}

// Release is the folder of release v's docs in cache: downloaded and
// unpacked there the first time, then kept. A folder of the cache holds a
// whole site or is not there: it is unpacked beside it, then renamed.
func Release(ctx context.Context, f Fetcher, v, cache string) (string, error) {
	dir := filepath.Join(cache, v)
	if IsSite(dir) {
		return dir, nil
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(cache, "."+v+"-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	archive, err := f.FetchDocs(ctx, "v"+v, tmp)
	if err != nil {
		return "", err
	}
	site := filepath.Join(tmp, "site")
	if err := Unpack(archive, site); err != nil {
		return "", fmt.Errorf("unpacking %s: %w", filepath.Base(archive), err)
	}
	if err := os.Rename(site, dir); err != nil {
		if IsSite(dir) {
			return dir, nil // another axx unpacked it first
		}
		return "", err
	}
	return dir, nil
}

// Open is the site a person gives: a built site's folder, or its archive,
// unpacked into a folder that cleanup removes.
func Open(p string) (dir string, cleanup func(), err error) {
	st, err := os.Stat(p)
	if err != nil {
		return "", nil, err
	}
	if st.IsDir() {
		if !IsSite(p) {
			return "", nil, ErrNotASite
		}
		return p, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "axx-docs-")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	dir = filepath.Join(tmp, "site")
	if err := Unpack(p, dir); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}
