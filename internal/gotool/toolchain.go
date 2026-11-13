package gotool

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/mod/module"

	"github.com/nimbusxr/axx/internal/mirror"
)

// The Go axx downloads is a Go release as the go command downloads one when a
// module needs a newer Go: the module golang.org/toolchain at
// v0.0.1-<release>.<os>-<arch>, from the Go module proxy (GOPROXY), checked
// against Go's checksum database (GOSUMDB, sumdb.go) and laid out as the go
// command lays it out (unpack.go).
const toolchainModule = "golang.org/toolchain"

// directToolchains is where GOPROXY's "direct" gets golang.org/toolchain: the
// module proxy its import path names
// (<meta name="go-import" content="golang.org/toolchain mod https://go.dev/dl/mod">).
const directToolchains = "https://go.dev/dl/mod"

// defaultGOPROXY is GOPROXY when nothing sets it, as Go's own go.env has it.
const defaultGOPROXY = "https://proxy.golang.org,direct"

// toolchainVersion is the version of golang.org/toolchain that is the Go
// release goVersion (go1.27.1) for this platform.
func toolchainVersion(goVersion string) string {
	return "v0.0.1-" + goVersion + "." + runtime.GOOS + "-" + runtime.GOARCH
}

// managed returns the downloaded toolchain's go, downloading it first if the
// cache has none.
func managed(ctx context.Context, o Options) (string, error) {
	name := fmt.Sprintf("%s.%s-%s", o.Version, runtime.GOOS, runtime.GOARCH)
	dir := filepath.Join(o.CacheDir, name)
	goBin := filepath.Join(dir, "go", "bin", goExe())
	if st, err := os.Stat(goBin); err == nil && st.Mode().IsRegular() {
		return goBin, nil
	}
	fmt.Fprintln(o.Log, "axx: setting up (once)")
	if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
		return "", err
	}
	vers := toolchainVersion(o.Version)
	archive, err := fetchToolchain(ctx, vers, o.CacheDir)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	if err := checkToolchain(ctx, archive, vers, o.CacheDir); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(o.CacheDir, name+".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := unpack(archive, vers, filepath.Join(tmp, "go")); err != nil {
		return "", fmt.Errorf("unpack Go %s: %w", o.Version, err)
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	return goBin, nil
}

// proxy is one entry of GOPROXY: an address, "direct" or "off", and whether
// any failure of it, not only its not having the module, moves on to the
// next entry (an entry followed by | rather than ,).
type proxy struct {
	url             string
	fallBackOnError bool
}

// proxies is the GOPROXY list, as the go command reads it.
func proxies() ([]proxy, error) {
	goproxy := goSetting("GOPROXY")
	if goproxy == "" {
		goproxy = defaultGOPROXY
	}
	var list []proxy
	for goproxy != "" {
		var u string
		fallBack := false
		if i := strings.IndexAny(goproxy, ",|"); i >= 0 {
			u, fallBack, goproxy = goproxy[:i], goproxy[i] == '|', goproxy[i+1:]
		} else {
			u, goproxy = goproxy, ""
		}
		u = strings.TrimSpace(u)
		switch {
		case u == "":
			continue
		case u == "off" || u == "direct":
			// Either ends the list.
			return append(list, proxy{url: u}), nil
		case strings.ContainsAny(u, ".:/") && !strings.Contains(u, ":/") && !filepath.IsAbs(u) && !path.IsAbs(u):
			// A host without a scheme, as the go command takes one.
			u = "https://" + u
		}
		if p, err := url.Parse(u); err != nil || (p.Scheme != "https" && p.Scheme != "http" && p.Scheme != "file") {
			return nil, fmt.Errorf("GOPROXY: %s is not a proxy's address (https://, http:// or file://)", mirror.Redact(u))
		}
		list = append(list, proxy{url: strings.TrimRight(u, "/"), fallBackOnError: fallBack})
	}
	if len(list) == 0 {
		return nil, errors.New("GOPROXY lists no proxy")
	}
	return list, nil
}

// fetchToolchain downloads golang.org/toolchain at vers from the first proxy
// that has it, into a file in dir, as the go command does: a proxy that does
// not have it (404 or 410) moves on to the next, and so does one that fails
// when | follows it.
func fetchToolchain(ctx context.Context, vers, dir string) (string, error) {
	list, err := proxies()
	if err != nil {
		return "", err
	}
	// A private module pattern that covers it sends it to its source.
	if pattern := cmp.Or(goSetting("GONOPROXY"), goSetting("GOPRIVATE")); pattern != "" && module.MatchPrefixPatterns(pattern, toolchainModule) {
		list = []proxy{{url: "direct"}}
	}
	escVers, err := module.EscapeVersion(vers)
	if err != nil {
		return "", err
	}
	var missing []string
	for _, p := range list {
		base := p.url
		switch base {
		case "off":
			return "", fmt.Errorf("GOPROXY=off: the go command, and axx, download no module, Go's own releases included%s", tried(missing))
		case "direct":
			base = directToolchains
		}
		file, err := download(ctx, base+"/"+toolchainModule+"/@v/"+escVers+".zip", dir)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, errNotFound) && !p.fallBackOnError {
			return "", err
		}
		missing = append(missing, err.Error())
	}
	return "", fmt.Errorf("no proxy GOPROXY lists has Go's %s@%s%s", toolchainModule, vers, tried(missing))
}

func tried(errs []string) string {
	if len(errs) == 0 {
		return ""
	}
	return ": " + strings.Join(errs, "; ")
}

// client downloads: a Go release is about 70 MB.
var client = &http.Client{Timeout: 10 * time.Minute}

// errNotFound is a proxy's answer that it has no such file (404 or 410), which
// moves on to the next proxy.
var errNotFound = errors.New("not found")

// statusError is a server's answer other than 200.
type statusError struct {
	addr, status string
	code         int
}

func (e *statusError) Error() string { return mirror.Redact(e.addr) + " answered " + e.status }

func (e *statusError) Is(target error) bool {
	return target == errNotFound && (e.code == http.StatusNotFound || e.code == http.StatusGone)
}

// open requests addr: a file:// address, or an http(s) one, with the
// credentials its address carries or, as the go command sends them, those
// .netrc has for its host.
func open(ctx context.Context, addr string) (io.ReadCloser, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return nil, fmt.Errorf("%s is not an address", mirror.Redact(addr))
	}
	if u.Scheme == "file" {
		f, err := os.Open(localPath(u))
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", addr, errNotFound)
		}
		return f, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return nil, mirror.RedactErr(err)
	}
	if u.User == nil && u.Scheme == "https" {
		if login, password, ok := netrcFor(u); ok {
			req.SetBasicAuth(login, password)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, mirror.RedactErr(err)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, &statusError{addr: addr, status: resp.Status, code: resp.StatusCode}
	}
	return resp.Body, nil
}

// localPath is a file:// address's file.
func localPath(u *url.URL) string {
	p := u.Path
	if runtime.GOOS == "windows" && len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:] // file:///C:/...
	}
	return filepath.FromSlash(p)
}

// get downloads addr, at most limit bytes.
func get(ctx context.Context, addr string, limit int64) ([]byte, error) {
	body, err := open(ctx, addr)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, mirror.RedactErr(err)
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is larger than %d bytes", mirror.Redact(addr), limit)
	}
	return b, nil
}

// download downloads addr into a new file in dir.
func download(ctx context.Context, addr, dir string) (string, error) {
	body, err := open(ctx, addr)
	if err != nil {
		return "", err
	}
	defer body.Close()
	f, err := os.CreateTemp(dir, "download-*.zip")
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("download %s: %w", mirror.Redact(addr), mirror.RedactErr(err))
	}
	return f.Name(), nil
}
