package npm

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// Package is a package of a lockfile, and where it goes.
type Package struct {
	// Path is where it goes below the install directory, as the lockfile
	// has it: node_modules/lighthouse, node_modules/a/node_modules/b.
	Path          string
	Name, Version string
	// Integrity is npm's integrity of the package's tarball (sha512-...).
	Integrity string
}

// InstallOptions configure Install.
type InstallOptions struct {
	// Lock is an npm lockfile, package-lock.json (lockfileVersion 2 or 3):
	// the packages its root needs are installed, and those they need.
	Lock []byte
	// LeaveOut names packages left out, with those only they need.
	LeaveOut []string

	// Dir is the directory the packages are installed in, once: Dir's
	// node_modules. Without it, they go in CacheDir, in a directory named
	// after Name and a hash of the packages (<Name>-<hash>), so that other
	// packages go elsewhere.
	Dir            string
	CacheDir, Name string // CacheDir: default CacheDir("")

	Registry     string // default Registry()
	GOOS, GOARCH string // the platform the packages are for; default the running one

	// Downloading, when set, is called before the downloads, when the
	// packages are not installed yet.
	Downloading func()
}

// Install installs the packages of a lockfile, once, and returns their
// directory. Each package's tarball is downloaded from the registry, 8 at a
// time, checked against the lockfile's integrity and unpacked in its place;
// the directory appears whole or not at all. Their lifecycle scripts
// (install, postinstall...) never run.
//
// A package built for some platforms only, as the lockfile's os, cpu and
// libc say (like @img/sharp-darwin-arm64, one of sharp's optional builds),
// is installed on those only: on another, it is left out, with those only
// it needs, when it is optional, and it is an error when it is not.
func Install(ctx context.Context, o InstallOptions) (string, error) {
	pkgs, err := Packages(o)
	if err != nil {
		return "", err
	}
	dir := o.Dir
	if dir == "" {
		if o.Name == "" {
			return "", errors.New("npm.Install needs a Dir, or a Name to name one after")
		}
		dir = filepath.Join(cmp.Or(o.CacheDir, CacheDir("")), installName(o.Name, pkgs))
	}
	registry := cmp.Or(strings.TrimRight(o.Registry, "/"), Registry())
	downloading := o.Downloading
	if downloading == nil {
		downloading = func() {}
	}
	if err := install(ctx, dir, registry, pkgs, downloading); err != nil {
		return "", err
	}
	return dir, nil
}

// Packages are the packages Install installs, in the order of their paths.
func Packages(o InstallOptions) ([]Package, error) {
	goos, goarch := o.GOOS, o.GOARCH
	if goos == "" {
		goos, goarch = runtime.GOOS, runtime.GOARCH
	}
	return packagesOf(o.Lock, o.LeaveOut, platformOf(goos, goarch))
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
	// The platforms the package is for, as its package.json says.
	OS   names `json:"os"`
	CPU  names `json:"cpu"`
	Libc names `json:"libc"`
	// Optional is npm's: every package that needs it needs it optionally.
	Optional bool `json:"optional"`
	// InBundle is a package another bundles: it comes, with what it needs, in
	// the bundling package's own tarball.
	InBundle bool `json:"inBundle"`
}

// packagesOf reads an npm lockfile (version 2 or 3): the packages its root
// needs, and those they need, found where Node.js finds them, in the
// nearest node_modules up the tree. A package named in leaveOut is left
// out, with those only it needs, and so is an optional package that is not
// for the platform on.
func packagesOf(lock []byte, leaveOut []string, on platform) ([]Package, error) {
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
	var out []Package
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
			if !dep.fits(on) {
				if dep.Optional {
					continue
				}
				return fmt.Errorf("the lockfile's %s is for %s only, not for %s, and is not optional", p, dep.platforms(), on)
			}
			if dep.InBundle {
				continue
			}
			if dep.Link {
				return fmt.Errorf("the lockfile links %s to a directory: only packages of a registry can be installed", p)
			}
			if !strings.HasPrefix(dep.Integrity, "sha512-") || dep.Version == "" {
				return fmt.Errorf("the lockfile has no version or no sha512 integrity for %s", p)
			}
			out = append(out, Package{Path: p, Name: nameOf(p, dep), Version: dep.Version, Integrity: dep.Integrity})
			if err := walk(p, dep); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk("", root); err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Package) int { return strings.Compare(a.Path, b.Path) })
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

// platform is a platform as Node.js names it (process.platform,
// process.arch), and as package.json's os, cpu and libc name platforms.
type platform struct{ os, cpu, libc string }

func (p platform) String() string { return p.os + " " + p.cpu }

// platformOf is a Go platform's Node.js names. Its libc is glibc on Linux:
// the Node.js builds of nodejs.org, which axx runs packages with, are glibc
// builds.
func platformOf(goos, goarch string) platform {
	p := platform{os: goos, cpu: goarch}
	switch goos {
	case "windows":
		p.os = "win32"
	case "solaris", "illumos":
		p.os = "sunos"
	case "linux":
		p.libc = "glibc"
	}
	if cpu, ok := map[string]string{"amd64": "x64", "386": "ia32", "ppc64le": "ppc64", "mipsle": "mipsel", "mips64le": "mips64el"}[goarch]; ok {
		p.cpu = cpu
	}
	return p
}

// names is package.json's os, cpu or libc: a list of names, or one.
type names []string

func (n *names) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*n = names{one}
		return nil
	}
	var list []string
	if err := json.Unmarshal(b, &list); err != nil {
		return err
	}
	*n = list
	return nil
}

// fits reports whether a package is for the platform, as npm checks it
// (npm-install-checks): its os, cpu and libc allow the platform's.
func (e lockEntry) fits(p platform) bool {
	return allows(e.OS, p.os) && allows(e.CPU, p.cpu) && (len(e.Libc) == 0 || p.libc != "" && allows(e.Libc, p.libc))
}

// allows reports whether a list of names of package.json allows value: it
// names it, or only names others, excluded ("!win32"). An empty list, or
// "any", allows every value.
func allows(list names, value string) bool {
	if len(list) == 0 || len(list) == 1 && list[0] == "any" {
		return true
	}
	excluded, named := 0, false
	for _, entry := range list {
		if name, ok := strings.CutPrefix(entry, "!"); ok {
			if name == value {
				return false
			}
			excluded++
		} else if entry == value {
			named = true
		}
	}
	return named || excluded == len(list)
}

// platforms says which platforms a package is for: "os darwin, cpu arm64".
func (e lockEntry) platforms() string {
	var s []string
	for _, f := range []struct {
		what string
		list names
	}{{"os", e.OS}, {"cpu", e.CPU}, {"libc", e.Libc}} {
		if len(f.list) > 0 {
			s = append(s, f.what+" "+strings.Join(f.list, " or "))
		}
	}
	return strings.Join(s, ", ")
}

// installName names the directory packages are installed in, after what
// and a hash of the packages, so that other packages go elsewhere.
func installName(what string, pkgs []Package) string {
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
func install(ctx context.Context, dir, registry string, pkgs []Package, downloading func()) error {
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
func fetchAll(ctx context.Context, registry string, pkgs []Package, dir string) error {
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
func fetchPackage(ctx context.Context, registry string, p Package, dir string) error {
	url := fmt.Sprintf("%s/%s/-/%s-%s.tgz", registry, p.Name, path.Base(p.Name), p.Version)
	body, err := Fetch(ctx, url)
	if err != nil {
		return err
	}
	if got := Integrity(body); got != p.Integrity {
		return fmt.Errorf("%s %s: integrity %s does not match the pinned %s", p.Name, p.Version, got, p.Integrity)
	}
	if err := Unpack(body, filepath.Join(dir, filepath.FromSlash(p.Path))); err != nil {
		return fmt.Errorf("%s %s: %w", p.Name, p.Version, err)
	}
	return nil
}
