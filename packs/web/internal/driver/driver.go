// Package driver prepares the Playwright driver the web-core pack runs: a Node.js
// binary and the playwright-core package, laid out the way playwright-go
// expects (<dir>/node and <dir>/package/cli.js), patched so that
// Playwright's Inspector and recorder speak the web-core pack's steps
// (patch.go). The browsers are not part of
// it: the driver downloads them, the first time the pack uses each one.
//
// Both downloads are checked against hashes pinned here, for exactly the
// versions the pack drives, so a compromised or corrupted mirror cannot slip
// in other code. The driver is prepared once and kept in axx's cache.
package driver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The versions the pack drives. CoreVersion is the Playwright version, whose
// browsers the pack runs.
const (
	CoreVersion = "1.62.1"
	NodeVersion = "24.19.0"
)

// coreIntegrity is npm's integrity of playwright-core-<CoreVersion>.tgz.
const coreIntegrity = "sha512-wPYSwEBJY9GHraISXqyqtx0na0LpO3XEX7jNDhntbex7tzUS7kLnZsOlFruFJB4Hi/rhDMjXGqHewDZ68nYZVw=="

// nodeSums are the SHA-256 sums of the Node.js archives, from
// https://nodejs.org/dist/v<NodeVersion>/SHASUMS256.txt.
var nodeSums = map[string]string{
	"darwin-arm64": "8294b7aa9b03997481c06babf1e8b270c859358f27da57a11509afe537ac381d",
	"darwin-x64":   "d1b5e999db158c62fe8f7267a4476b035d8bd93b1a605bac24a3f0dd166e3316",
	"linux-arm64":  "d28c8a5bf0a808f0ed434a1dce8c54ae98f0371c0bd86ac58abc613f73e6643f",
	"linux-x64":    "f625d97cd707df4ff96254916fbc5ff014f09c09effe5a1e0ca8f6d41a8789d4",
	"win-arm64":    "8502f4a50b458d4cc38ed8f2001556c2cd239d464920f74017926ccb1e1c157f",
	"win-x64":      "57f71ab3652e797d84acddc79c81cc9ff1c6ddb2a1974cdb83f00fee9bff4c73",
}

// Options configure Ensure; the zero value downloads from nodejs.org and
// registry.npmjs.org into axx's cache. NODE_MIRROR and npm_config_registry
// point the downloads at mirrors; the pinned hashes still apply.
type Options struct {
	CacheDir     string // default <user cache>/axx/web
	NodeMirror   string // default $NODE_MIRROR or https://nodejs.org/dist
	NPMRegistry  string // default $npm_config_registry or https://registry.npmjs.org
	GOOS, GOARCH string // default the running platform

	// Tests only: other versions and hashes.
	coreVersion, nodeVersion, coreIntegrity string
	nodeSums                                map[string]string
}

var client = &http.Client{Timeout: 10 * time.Minute}

// Ensure returns the driver directory, preparing it on first use.
func Ensure(ctx context.Context, o Options) (string, error) {
	o = o.defaults()
	platform, ext, err := nodePlatform(o.GOOS, o.GOARCH)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("playwright-%s-node-%s-%s", o.coreVersion, o.nodeVersion, platform)
	if o.coreVersion == CoreVersion {
		name = fmt.Sprintf("playwright-%s-axx%d-node-%s-%s", o.coreVersion, patchRevision, o.nodeVersion, platform)
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
	archive := fmt.Sprintf("node-v%s-%s.%s", o.nodeVersion, platform, ext)
	body, err := fetch(ctx, o.NodeMirror+"/v"+o.nodeVersion+"/"+archive)
	if err != nil {
		return "", err
	}
	if got := sha256Hex(body); got != o.nodeSums[platform] {
		return "", fmt.Errorf("%s: SHA-256 %s does not match the pinned %s", archive, got, o.nodeSums[platform])
	}
	root := strings.TrimSuffix(archive, "."+ext)
	if ext == "zip" {
		err = unzipOne(body, root+"/node.exe", filepath.Join(tmp, "node.exe"))
	} else {
		err = untarOne(body, root+"/bin/node", filepath.Join(tmp, "node"))
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", archive, err)
	}

	// playwright-core: the whole package, under package/.
	tgz := fmt.Sprintf("playwright-core-%s.tgz", o.coreVersion)
	body, err = fetch(ctx, o.NPMRegistry+"/playwright-core/-/"+tgz)
	if err != nil {
		return "", err
	}
	if got := integrity(body); got != o.coreIntegrity {
		return "", fmt.Errorf("%s: integrity %s does not match the pinned %s", tgz, got, o.coreIntegrity)
	}
	if err := untarPackage(body, tmp); err != nil {
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
		if base, err := os.UserCacheDir(); err == nil {
			o.CacheDir = filepath.Join(base, "axx", "web")
		} else {
			o.CacheDir = filepath.Join(os.TempDir(), "axx-web")
		}
	}
	if o.NodeMirror == "" {
		o.NodeMirror = envOr("NODE_MIRROR", "https://nodejs.org/dist")
	}
	if o.NPMRegistry == "" {
		o.NPMRegistry = envOr("npm_config_registry", "https://registry.npmjs.org")
	}
	o.NodeMirror = strings.TrimRight(o.NodeMirror, "/")
	o.NPMRegistry = strings.TrimRight(o.NPMRegistry, "/")
	if o.GOOS == "" {
		o.GOOS, o.GOARCH = runtime.GOOS, runtime.GOARCH
	}
	if o.coreVersion == "" {
		o.coreVersion, o.nodeVersion, o.coreIntegrity, o.nodeSums = CoreVersion, NodeVersion, coreIntegrity, nodeSums
	}
	return o
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// nodePlatform names the Node.js archive for a platform ("darwin-arm64") and
// its extension.
func nodePlatform(goos, goarch string) (platform, ext string, err error) {
	osName := map[string]string{"darwin": "darwin", "linux": "linux", "windows": "win"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if osName == "" || arch == "" {
		return "", "", fmt.Errorf("the web-core pack's browser driver needs Node.js, which has no build for %s/%s", goos, goarch)
	}
	ext = "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return osName + "-" + arch, ext, nil
}

func ready(dir, goos string) bool {
	node := "node"
	if goos == "windows" {
		node = "node.exe"
	}
	for _, f := range []string{node, filepath.Join("package", "cli.js")} {
		if st, err := os.Stat(filepath.Join(dir, f)); err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

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

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// integrity is npm's Subresource Integrity form of b: "sha512-<base64>".
func integrity(b []byte) string {
	h := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(h[:])
}

func untarOne(archive []byte, name, dest string) error {
	found := false
	err := eachTar(archive, func(h *tar.Header, r io.Reader) error {
		if h.Name != name || h.Typeflag != tar.TypeReg {
			return nil
		}
		found = true
		return write(dest, r, 0o755)
	})
	if err == nil && !found {
		err = fmt.Errorf("%s is not in the archive", name)
	}
	return err
}

// untarPackage extracts an npm tarball's package/ directory into dir.
func untarPackage(archive []byte, dir string) error {
	found := false
	err := eachTar(archive, func(h *tar.Header, r io.Reader) error {
		if h.Typeflag != tar.TypeReg || !strings.HasPrefix(h.Name, "package/") {
			return nil
		}
		dest, err := within(dir, h.Name)
		if err != nil {
			return err
		}
		found = true
		return write(dest, r, h.FileInfo().Mode().Perm()|0o600)
	})
	if err == nil && !found {
		err = errors.New("the archive holds no package/ directory")
	}
	return err
}

func eachTar(archive []byte, fn func(*tar.Header, io.Reader) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
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
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

func unzipOne(archive []byte, name, dest string) error {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return write(dest, rc, 0o755)
	}
	return fmt.Errorf("%s is not in the archive", name)
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
