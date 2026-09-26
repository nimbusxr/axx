package lighthouse

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// lighthouseVersion is the Lighthouse the pack runs (Apache-2.0, Google):
// downloaded from npm, never distributed with axx. npm/package-lock.json
// pins it and every package it loads, each with its integrity
// (TestTheLockfilePinsLighthouse checks that they agree).
//
// Lighthouse drives the browser through its DevTools protocol, so its
// version must follow the Chromium of the Playwright the web-core pack drives
// (driver.CoreVersion): 13.5.0 audits Chromium 151, Playwright 1.62.1's.
// To upgrade it, change npm/package.json and run
// `npm install --package-lock-only --ignore-scripts` in npm/.
const lighthouseVersion = "13.5.0"

//go:embed npm/package-lock.json
var lockfile []byte

// unused are the packages Lighthouse depends on but never loads here, left
// out with those only they need: Sentry sends Lighthouse's errors to its
// authors when asked to, which axx never does.
var unused = []string{"@sentry/node"}

// npmPackage is a package of a lockfile, and where it goes.
type npmPackage struct {
	// Path is where it goes below the install directory, as the lockfile
	// has it: node_modules/lighthouse, node_modules/a/node_modules/b.
	Path          string
	Name, Version string
	// Integrity is npm's integrity of the package's tarball (sha512-...).
	Integrity string
}

type lockEntry struct {
	// Name is the package's, where it differs from its path's (an alias).
	Name                 string            `json:"name"`
	Version              string            `json:"version"`
	Integrity            string            `json:"integrity"`
	Link                 bool              `json:"link"`
	Dependencies         map[string]string `json:"dependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
}

// packagesOf reads an npm lockfile (version 2 or 3): the packages its root
// needs, and those they need, found where Node.js finds them, in the
// nearest node_modules up the tree. A package named in leaveOut is left
// out, with those only it needs.
func packagesOf(lock []byte, leaveOut []string) ([]npmPackage, error) {
	var lf struct {
		LockfileVersion int                  `json:"lockfileVersion"`
		Packages        map[string]lockEntry `json:"packages"`
	}
	if err := json.Unmarshal(lock, &lf); err != nil {
		return nil, fmt.Errorf("the lockfile is no JSON: %w", err)
	}
	if lf.LockfileVersion < 2 {
		return nil, fmt.Errorf("the lockfile is of version %d, not 2 or 3", lf.LockfileVersion)
	}
	root, ok := lf.Packages[""]
	if !ok {
		return nil, errors.New("the lockfile has no root package")
	}
	seen := map[string]bool{}
	var out []npmPackage
	var walk func(from string, e lockEntry) error
	walk = func(from string, e lockEntry) error {
		needs := map[string]bool{}
		for name := range e.PeerDependencies {
			needs[name] = false // installed or not
		}
		for name := range e.OptionalDependencies {
			needs[name] = false
		}
		for name := range e.Dependencies {
			needs[name] = true
		}
		for _, name := range slices.Sorted(maps.Keys(needs)) {
			if slices.Contains(leaveOut, name) {
				continue
			}
			p, dep, found := resolve(lf.Packages, from, name)
			if !found {
				if needs[name] {
					who := nameOf(from, e)
					if who == "" {
						who = "the root package"
					}
					return fmt.Errorf("the lockfile lacks %s, which %s needs", name, who)
				}
				continue
			}
			if seen[p] {
				continue
			}
			seen[p] = true
			if dep.Link {
				return fmt.Errorf("the lockfile links %s to a directory: only packages of a registry can be installed", p)
			}
			if !strings.HasPrefix(dep.Integrity, "sha512-") || dep.Version == "" {
				return fmt.Errorf("the lockfile has no version or no sha512 integrity for %s", p)
			}
			out = append(out, npmPackage{Path: p, Name: nameOf(p, dep), Version: dep.Version, Integrity: dep.Integrity})
			if err := walk(p, dep); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("", root); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b npmPackage) int { return strings.Compare(a.Path, b.Path) })
	return out, nil
}

// resolve finds a package the package at from needs, as Node.js does: in
// from's node_modules, then in those of the packages from is in, up to the
// root's.
func resolve(pkgs map[string]lockEntry, from, name string) (string, lockEntry, bool) {
	for base := from; ; {
		p := "node_modules/" + name
		if base != "" {
			p = base + "/" + p
		}
		if e, ok := pkgs[p]; ok {
			return p, e, true
		}
		if base == "" {
			return "", lockEntry{}, false
		}
		if i := strings.LastIndex(base, "/node_modules/"); i >= 0 {
			base = base[:i]
		} else {
			base = ""
		}
	}
}

// nameOf is the name of the package at p: its path's, below its last
// node_modules, unless the lockfile names it otherwise.
func nameOf(p string, e lockEntry) string {
	if e.Name != "" {
		return e.Name
	}
	const nm = "node_modules/"
	if i := strings.LastIndex(p, nm); i >= 0 {
		return p[i+len(nm):]
	}
	return p
}

// installName names the directory packages are installed in, after what
// and a hash of the packages, so that other packages go elsewhere.
func installName(what string, pkgs []npmPackage) string {
	h := sha256.New()
	for _, p := range pkgs {
		fmt.Fprintf(h, "%s %s\n", p.Path, p.Integrity)
	}
	return what + "-" + hex.EncodeToString(h.Sum(nil))[:8]
}

// install installs packages in dir, once: each package's tarball is
// downloaded from registry, checked against its integrity and unpacked in
// its place. dir appears whole or not at all. downloading is called before
// the downloads.
func install(ctx context.Context, dir, registry string, pkgs []npmPackage, downloading func()) error {
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return nil
	}
	downloading()
	parent := filepath.Dir(dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, "prepare-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err := fetchAll(ctx, registry, pkgs, tmp); err != nil {
		return err
	}
	// Another axx may have installed them meanwhile: keep the first.
	if err := os.Rename(tmp, dir); err != nil {
		if st, serr := os.Stat(dir); serr == nil && st.IsDir() {
			return nil
		}
		return err
	}
	return nil
}

// fetchAll downloads and unpacks packages into dir, a few at a time; the
// first error stops them.
func fetchAll(ctx context.Context, registry string, pkgs []npmPackage, dir string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	turns := make(chan struct{}, 8)
	for _, p := range pkgs {
		wg.Go(func() {
			turns <- struct{}{}
			defer func() { <-turns }()
			if ctx.Err() != nil {
				return
			}
			if err := fetchPackage(ctx, registry, p, dir); err != nil {
				mu.Lock()
				if first == nil {
					first = err
					cancel()
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if first == nil {
		first = ctx.Err()
	}
	return first
}

// fetchPackage downloads one package, checks it and unpacks it.
func fetchPackage(ctx context.Context, registry string, p npmPackage, dir string) error {
	url := fmt.Sprintf("%s/%s/-/%s-%s.tgz", registry, p.Name, path.Base(p.Name), p.Version)
	body, err := fetch(ctx, url)
	if err != nil {
		return err
	}
	if got := integrity(body); got != p.Integrity {
		return fmt.Errorf("%s %s: integrity %s does not match the pinned %s", p.Name, p.Version, got, p.Integrity)
	}
	if err := unpack(body, filepath.Join(dir, filepath.FromSlash(p.Path))); err != nil {
		return fmt.Errorf("%s %s: %w", p.Name, p.Version, err)
	}
	return nil
}

var client = &http.Client{Timeout: 5 * time.Minute}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// integrity is npm's Subresource Integrity form of b: "sha512-<base64>".
func integrity(b []byte) string {
	h := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(h[:])
}

// unpack unpacks an npm tarball into dir, as npm does: its files, without
// the directory they are in (package/, mostly). Links are left out.
func unpack(tgz []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimPrefix(h.Name, "./"), "/")
		if !ok || name == "" {
			continue
		}
		dest, err := within(dir, name)
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		if err := write(dest, tr, mode); err != nil {
			return err
		}
	}
}

// within joins an archive entry to dir, refusing entries that leave it.
func within(dir, name string) (string, error) {
	p := filepath.Join(dir, filepath.FromSlash(name))
	if !strings.HasPrefix(p, filepath.Clean(dir)+string(filepath.Separator)) {
		return "", fmt.Errorf("archive entry %q leaves the directory", name)
	}
	return p, nil
}

func write(dest string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil { //nolint:gosec // a checksummed archive
		_ = f.Close()
		return err
	}
	return f.Close()
}

// writeOnce writes a file that another axx may be writing too: aside,
// then renamed, unless it is there already.
func writeOnce(dest string, content []byte) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "write-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}
