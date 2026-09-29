// Package driver prepares the Playwright driver the web-core pack runs: a Node.js
// binary and the playwright-core package, laid out the way playwright-go
// expects (<dir>/node and <dir>/package/cli.js), patched so that
// Playwright's Inspector and recorder speak the web-core pack's steps
// (patch.go). The browsers are not part of
// it: the driver downloads them, the first time the pack uses each one.
//
// Both downloads are checked against hashes pinned for exactly the versions
// the pack drives (Node.js's in internal/npm), so a compromised or corrupted
// mirror cannot slip in other code. The driver is prepared once and kept in
// axx's cache.
package driver

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/nimbusxr/axx/internal/npm"
)

// CoreVersion is the Playwright version the pack drives, whose browsers the
// pack runs. The Node.js it runs with is npm.NodeVersion.
const CoreVersion = "1.62.1"

// coreIntegrity is npm's integrity of playwright-core-<CoreVersion>.tgz.
const coreIntegrity = "sha512-wPYSwEBJY9GHraISXqyqtx0na0LpO3XEX7jNDhntbex7tzUS7kLnZsOlFruFJB4Hi/rhDMjXGqHewDZ68nYZVw=="

// Options configure Ensure; the zero value downloads from nodejs.org and
// registry.npmjs.org into axx's cache. NODE_MIRROR and npm_config_registry
// point the downloads at mirrors; the pinned hashes still apply.
type Options struct {
	CacheDir     string // default <user cache>/axx/web
	NodeMirror   string // default $NODE_MIRROR or https://nodejs.org/dist
	NPMRegistry  string // default $npm_config_registry or https://registry.npmjs.org
	GOOS, GOARCH string // default the running platform

	// Tests only: other versions and hashes.
	coreVersion, coreIntegrity string
	node                       npm.NodeRelease
}

// Ensure returns the driver directory, preparing it on first use.
func Ensure(ctx context.Context, o Options) (string, error) {
	o = o.defaults()
	platform, err := npm.NodePlatform(o.GOOS, o.GOARCH)
	if err != nil {
		return "", fmt.Errorf("the web-core pack's browser driver needs Node.js, which has no build for %s/%s", o.GOOS, o.GOARCH)
	}
	nodeVersion := cmp.Or(o.node.Version, npm.NodeVersion)
	name := fmt.Sprintf("playwright-%s-node-%s-%s", o.coreVersion, nodeVersion, platform)
	if o.coreVersion == CoreVersion {
		name = fmt.Sprintf("playwright-%s-axx%d-node-%s-%s", o.coreVersion, patchRevision, nodeVersion, platform)
	}
	dir := filepath.Join(o.CacheDir, name)
	if ready(dir, o.GOOS) {
		return dir, nil
	}
	if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(o.CacheDir, "prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	// Node.js: only its binary is needed.
	nodeOptions := npm.Options{NodeMirror: o.NodeMirror, GOOS: o.GOOS, GOARCH: o.GOARCH, Node: o.node}
	if _, err := npm.WriteNode(ctx, nodeOptions, tmp); err != nil {
		return "", err
	}

	// playwright-core: the whole package, under package/.
	tgz := fmt.Sprintf("playwright-core-%s.tgz", o.coreVersion)
	body, err := npm.Fetch(ctx, o.NPMRegistry+"/playwright-core/-/"+tgz)
	if err != nil {
		return "", err
	}
	if got := npm.Integrity(body); got != o.coreIntegrity {
		return "", fmt.Errorf("%s: integrity %s does not match the pinned %s", tgz, got, o.coreIntegrity)
	}
	if err := npm.Unpack(body, filepath.Join(tmp, "package")); err != nil {
		return "", fmt.Errorf("%s: %w", tgz, err)
	}
	// The patches are for the pinned version.
	if o.coreVersion == CoreVersion {
		if err := applyPatches(filepath.Join(tmp, "package")); err != nil {
			return "", err
		}
	}

	// Another axx may have prepared it meanwhile: keep the first.
	if err := os.Rename(tmp, dir); err != nil {
		if ready(dir, o.GOOS) {
			return dir, nil
		}
		return "", err
	}
	return dir, nil
}

func (o Options) defaults() Options {
	if o.CacheDir == "" {
		o.CacheDir = npm.CacheDir("web")
	}
	if o.NPMRegistry == "" {
		o.NPMRegistry = npm.Registry()
	}
	o.NPMRegistry = strings.TrimRight(o.NPMRegistry, "/")
	if o.GOOS == "" {
		o.GOOS, o.GOARCH = runtime.GOOS, runtime.GOARCH
	}
	if o.coreVersion == "" {
		o.coreVersion, o.coreIntegrity = CoreVersion, coreIntegrity
	}
	return o
}

func ready(dir, goos string) bool {
	for _, f := range []string{npm.NodeName(goos), filepath.Join("package", "cli.js")} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}
