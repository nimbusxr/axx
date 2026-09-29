package npm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// NodeVersion is the Node.js axx runs npm packages with: the web-core
// pack's Playwright driver, Lighthouse.
const NodeVersion = "24.19.0"

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

// NodeRelease is a Node.js release: its version, and the SHA-256 sums of
// its archives by platform (NodePlatform), as its SHASUMS256.txt lists them.
type NodeRelease struct {
	Version string
	Sums    map[string]string
}

// NodePlatform names the platform of a Node.js archive for a Go platform:
// darwin-arm64, linux-x64, win-x64...
func NodePlatform(goos, goarch string) (string, error) {
	platform, _, err := nodeArchive(goos, goarch)
	return platform, err
}

// nodeArchive names the Node.js archive's platform and its extension.
func nodeArchive(goos, goarch string) (platform, ext string, err error) {
	osName := map[string]string{"darwin": "darwin", "linux": "linux", "windows": "win"}[goos]
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	if osName == "" || arch == "" {
		return "", "", fmt.Errorf("there is no Node.js for %s/%s", goos, goarch)
	}
	ext = "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return osName + "-" + arch, ext, nil
}

// NodeName is the file name of the Node.js binary on goos: node, or
// node.exe on Windows.
func NodeName(goos string) string {
	if goos == "windows" {
		return "node.exe"
	}
	return "node"
}

// EnsureNode returns the path of the Node.js binary for the platform,
// downloading it the first time into <CacheDir>/node-v<version>-<platform>.
func EnsureNode(ctx context.Context, o Options) (string, error) {
	o = o.defaults()
	platform, err := NodePlatform(o.GOOS, o.GOARCH)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(o.CacheDir, "node-v"+o.Node.Version+"-"+platform)
	node := filepath.Join(dir, NodeName(o.GOOS))
	if isFile(node) {
		return node, nil
	}
	if err := os.MkdirAll(o.CacheDir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(o.CacheDir, "prepare-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if _, err := WriteNode(ctx, o, tmp); err != nil {
		return "", err
	}
	// Another axx may have prepared it meanwhile: keep the first.
	if err := os.Rename(tmp, dir); err != nil {
		if isFile(node) {
			return node, nil
		}
		return "", err
	}
	return node, nil
}

// WriteNode downloads the Node.js archive for the platform, checks it
// against its pinned SHA-256 sum and writes its binary alone into dir, as
// NodeName: for a directory that holds Node.js beside what it runs, like
// Playwright's driver. It returns the binary's path.
func WriteNode(ctx context.Context, o Options, dir string) (string, error) {
	o = o.defaults()
	platform, ext, err := nodeArchive(o.GOOS, o.GOARCH)
	if err != nil {
		return "", err
	}
	archive := fmt.Sprintf("node-v%s-%s.%s", o.Node.Version, platform, ext)
	body, err := Fetch(ctx, o.NodeMirror+"/v"+o.Node.Version+"/"+archive)
	if err != nil {
		return "", err
	}
	if got := sha256Hex(body); got != o.Node.Sums[platform] {
		return "", fmt.Errorf("%s: SHA-256 %s does not match the pinned %s", archive, got, o.Node.Sums[platform])
	}
	root := strings.TrimSuffix(archive, "."+ext)
	dest := filepath.Join(dir, NodeName(o.GOOS))
	if ext == "zip" {
		err = unzipOne(body, root+"/node.exe", dest)
	} else {
		err = UnpackFile(body, root+"/bin/node", dest)
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", archive, err)
	}
	return dest, nil
}

func isFile(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.Mode().IsRegular()
}
