// Package gotool provides the Go toolchain axx builds itself with when a
// project lists packs. Nobody has to install Go: axx downloads the release
// of Go it was built with, once, checks its SHA-256 against go.dev, and
// keeps it in its cache. Builds run with the toolchain's caches in axx's
// cache too, so they leave nothing in the user's home.
package gotool

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Toolchain is a go command and the environment to run it in.
type Toolchain struct {
	Go  string
	Env []string
}

// Options configure Ensure; the zero value is the default.
type Options struct {
	// CacheDir holds the toolchain and its caches; default
	// <user cache>/axx/go.
	CacheDir string
	// Version is the Go release to use; default the one axx was built with.
	Version string
	// DownloadURL is where Go releases are listed and downloaded; default
	// https://go.dev/dl/.
	DownloadURL string
	// Log receives a line while a download runs.
	Log io.Writer
}

// Ensure returns the toolchain to build with: AXX_GO when set, else the
// release axx was built with (downloaded on first use), else go on PATH.
func Ensure(ctx context.Context, o Options) (*Toolchain, error) {
	if o.Log == nil {
		o.Log = io.Discard
	}
	if p := os.Getenv("AXX_GO"); p != "" {
		return &Toolchain{Go: p, Env: baseEnv()}, nil
	}
	if o.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		o.CacheDir = filepath.Join(base, "axx", "go")
	}
	if o.Version == "" {
		o.Version = runtime.Version()
	}
	if o.DownloadURL == "" {
		o.DownloadURL = "https://go.dev/dl/"
	}
	if !strings.HasPrefix(o.Version, "go") || strings.Contains(o.Version, "devel") {
		// A development toolchain has no release to download.
		return onPath(errors.New("axx was built with a development version of Go"))
	}
	goBin, err := managed(ctx, o)
	if err != nil {
		return onPath(err)
	}
	return &Toolchain{Go: goBin, Env: append(baseEnv(),
		"GOTOOLCHAIN=local",
		"GOPATH="+filepath.Join(o.CacheDir, "path"),
		"GOMODCACHE="+filepath.Join(o.CacheDir, "path", "pkg", "mod"),
		"GOCACHE="+filepath.Join(o.CacheDir, "build"),
		"GOENV=off",
	)}, nil
}

// baseEnv is what every build runs with: static binaries, no telemetry, no
// workspace.
func baseEnv() []string {
	return append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-mod=mod", "GOTELEMETRY=off")
}

// onPath falls back to an installed go, or explains why there is none.
func onPath(cause error) (*Toolchain, error) {
	p, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("cannot get the toolchain axx prepares packs with: %w", cause)
	}
	return &Toolchain{Go: p, Env: baseEnv()}, nil
}

// managed returns the downloaded toolchain's go, downloading it first if
// the cache has none.
func managed(ctx context.Context, o Options) (string, error) {
	name := fmt.Sprintf("%s.%s-%s", o.Version, runtime.GOOS, runtime.GOARCH)
	dir := filepath.Join(o.CacheDir, name)
	goBin := filepath.Join(dir, "go", "bin", goExe())
	if st, err := os.Stat(goBin); err == nil && st.Mode().IsRegular() {
		return goBin, nil
	}
	file, sum, err := release(ctx, o)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(o.Log, "axx: setting up (once)")
	if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
		return "", err
	}
	archive, err := download(ctx, o.DownloadURL+file, sum, o.CacheDir)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)
	tmp, err := os.MkdirTemp(o.CacheDir, name+".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if strings.HasSuffix(file, ".zip") {
		err = unzip(archive, tmp)
	} else {
		err = untar(archive, tmp)
	}
	if err != nil {
		return "", fmt.Errorf("unpack %s: %w", file, err)
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return "", err
	}
	return goBin, nil
}

// release finds the archive of the version for this platform and its
// SHA-256 in go.dev's list of releases.
func release(ctx context.Context, o Options) (file, sum string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.DownloadURL+"?mode=json&include=all", nil)
	if err != nil {
		return "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("list Go releases: %s", resp.Status)
	}
	var releases []struct {
		Version string `json:"version"`
		Files   []struct {
			Filename string `json:"filename"`
			OS       string `json:"os"`
			Arch     string `json:"arch"`
			SHA256   string `json:"sha256"`
			Kind     string `json:"kind"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", "", fmt.Errorf("list Go releases: %w", err)
	}
	for _, r := range releases {
		if r.Version != o.Version {
			continue
		}
		for _, f := range r.Files {
			if f.Kind == "archive" && f.OS == runtime.GOOS && f.Arch == runtime.GOARCH {
				return f.Filename, f.SHA256, nil
			}
		}
	}
	return "", "", fmt.Errorf("no %s release of Go for %s/%s", o.Version, runtime.GOOS, runtime.GOARCH)
}

var client = &http.Client{Timeout: 10 * time.Minute}

// download fetches url into dir, checking its SHA-256.
func download(ctx context.Context, url, sum, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: %s", url, resp.Status)
	}
	f, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && hex.EncodeToString(h.Sum(nil)) != sum {
		err = fmt.Errorf("download %s: the SHA-256 does not match go.dev's", url)
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func untar(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := within(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := write(target, tr, h.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil {
				return err
			}
		}
	}
}

func unzip(archive, dir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, zf := range r.File {
		target, err := within(dir, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = write(target, rc, zf.Mode())
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// within joins an archive entry to dir, refusing entries that escape it.
func within(dir, name string) (string, error) {
	target := filepath.Join(dir, filepath.FromSlash(name))
	if target != dir && !strings.HasPrefix(target, dir+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q leaves the directory", name)
	}
	return target, nil
}

func write(target string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm()|0o200)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil { //nolint:gosec // a checksummed Go release
		_ = f.Close()
		return err
	}
	return f.Close()
}

// goExe is the file name of the go command.
func goExe() string {
	if runtime.GOOS == "windows" {
		return "go.exe"
	}
	return "go"
}
