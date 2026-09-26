package driver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Package is one file of a pinned npm package: a browser library a pack
// injects into pages, such as axe-core's axe.min.js.
type Package struct {
	Name, Version string
	// Integrity is npm's integrity of the package's tarball (sha512-...).
	Integrity string
	// File is the file of the package, below package/.
	File string
}

// Fetch returns the path of the package's file, downloading the package once
// into axx's cache and checking it against its integrity. NPMRegistry
// (npm_config_registry) points the download at a mirror.
func (p Package) Fetch(ctx context.Context, o Options) (string, error) {
	o = o.defaults()
	dir := filepath.Join(o.CacheDir, p.Name+"-"+p.Version)
	path := filepath.Join(dir, filepath.FromSlash(p.File))
	if st, err := os.Stat(path); err == nil && st.Mode().IsRegular() {
		return path, nil
	}
	tgz := fmt.Sprintf("%s-%s.tgz", p.Name, p.Version)
	body, err := fetch(ctx, o.NPMRegistry+"/"+p.Name+"/-/"+tgz)
	if err != nil {
		return "", err
	}
	if got := integrity(body); got != p.Integrity {
		return "", fmt.Errorf("%s: integrity %s does not match the pinned %s", tgz, got, p.Integrity)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// Written aside, then renamed: another axx may be fetching it too.
	tmp, err := os.CreateTemp(dir, "fetch-")
	if err != nil {
		return "", err
	}
	_ = tmp.Close()
	defer os.Remove(tmp.Name())
	if err := untarOne(body, "package/"+strings.TrimPrefix(p.File, "/"), tmp.Name()); err != nil {
		return "", fmt.Errorf("%s: %w", tgz, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
