// Package npm prepares what packs run with Node.js: a pinned Node.js binary
// (EnsureNode, WriteNode), and pinned npm packages, from a package-lock.json
// (Install) or one at a time (Fetch, Unpack).
//
// Everything is checked against hashes pinned in axx or in the lockfile, so
// that a compromised or corrupted mirror cannot slip in other code, and is
// kept in axx's cache, prepared aside and renamed into place: when two axx
// prepare the same thing at once, the first one's stays. No package's
// lifecycle scripts ever run.
package npm

import (
	"cmp"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Options configure EnsureNode and WriteNode; the zero value downloads the
// pinned Node.js (NodeVersion) from nodejs.org, for the running platform,
// into axx's cache. NODE_MIRROR points the download at a mirror; the pinned
// hashes still apply.
type Options struct {
	CacheDir     string // default CacheDir("")
	NodeMirror   string // default $NODE_MIRROR or https://nodejs.org/dist
	GOOS, GOARCH string // default the running platform
	// Node is the Node.js release; the zero value is the pinned one.
	Node NodeRelease
}

func (o Options) defaults() Options {
	if o.CacheDir == "" {
		o.CacheDir = CacheDir("")
	}
	o.NodeMirror = strings.TrimRight(cmp.Or(o.NodeMirror, os.Getenv("NODE_MIRROR"), "https://nodejs.org/dist"), "/")
	if o.GOOS == "" {
		o.GOOS, o.GOARCH = runtime.GOOS, runtime.GOARCH
	}
	if o.Node.Version == "" {
		o.Node = NodeRelease{Version: NodeVersion, Sums: nodeSums}
	}
	return o
}

// CacheDir is axx's cache directory for sub: <user cache>/axx/<sub>, or
// <temp>/axx-<sub> on a machine without a user cache directory.
func CacheDir(sub string) string {
	if base, err := os.UserCacheDir(); err == nil {
		return filepath.Join(base, "axx", sub)
	}
	if sub == "" {
		return filepath.Join(os.TempDir(), "axx")
	}
	return filepath.Join(os.TempDir(), "axx-"+sub)
}

// Registry is the npm registry packages are downloaded from:
// $npm_config_registry, or npm's own, without a trailing slash.
func Registry() string {
	return strings.TrimRight(cmp.Or(os.Getenv("npm_config_registry"), "https://registry.npmjs.org"), "/")
}

var client = &http.Client{Timeout: 10 * time.Minute}

// Fetch downloads url.
func Fetch(ctx context.Context, url string) ([]byte, error) {
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

// Integrity is npm's Subresource Integrity form of b: "sha512-<base64>",
// as package-lock.json pins packages' tarballs.
func Integrity(b []byte) string {
	h := sha512.Sum512(b)
	return "sha512-" + base64.StdEncoding.EncodeToString(h[:])
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
