// Package video keeps what a scenario's desktop showed as a video: H.264 in
// an MP4 file, which plays wherever a video plays. Cisco's OpenH264 encodes
// it: axx downloads Cisco's build from Cisco the first time it keeps a video,
// as Cisco's binary license has it, and packs.desktop-core.videos (never)
// turns it off. OpenH264 Video Codec provided by Cisco Systems, Inc.
package video

import (
	"bytes"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Credit is the line Cisco's binary license asks of software that uses
// OpenH264, wherever it says how to turn the codec off.
const Credit = "OpenH264 Video Codec provided by Cisco Systems, Inc."

// openh264Version is the OpenH264 release axx downloads.
const openh264Version = "2.6.0"

// openh264URL is where Cisco publishes its builds of OpenH264.
var openh264URL = "https://ciscobinary.openh264.org/"

// openh264Builds are Cisco's builds of the release for each OS and
// architecture, with the SHA-256 of each file as Cisco serves it.
var openh264Builds = map[string]struct{ file, sha256 string }{
	"darwin/amd64":  {"libopenh264-2.6.0-mac-x64.dylib.bz2", "38b2ed6d1d45b6a3e408c734173f2d67ab44a10d0e154ff3489b89877cd60e7e"},
	"darwin/arm64":  {"libopenh264-2.6.0-mac-arm64.dylib.bz2", "6db362ee5abdab572311aeadb96d3f44b0617d9a4a4b9f4db4cb5ac4d968da71"},
	"linux/amd64":   {"libopenh264-2.6.0-linux64.8.so.bz2", "27ab53323c110b76214c1c72222f459d17febbcd1e252136cadc292b0308d75b"},
	"linux/arm64":   {"libopenh264-2.6.0-linux-arm64.8.so.bz2", "a78aea7970150f46bcd3bb7994c9e6dd90bd7a9ea785920f5a73f6964e3fcda7"},
	"windows/amd64": {"openh264-2.6.0-win64.dll.bz2", "dab5f2a872777f9a58b69bfa9fbcf20d9f82f2d6ec91383fd70bff49bd34ac9f"},
	"windows/arm64": {"openh264-2.6.0-win-arm64.dll.bz2", "6b57d3ecd06bd80b5f3ee1abbb31d48798ca6d89ef1bc98cf2e6d1ebddf5cf45"},
}

// library is OpenH264, loaded: the functions that make and free an encoder.
type library struct {
	create, destroy uintptr
}

var (
	loaded    *library
	loadErr   error
	loadOnce  sync.Once
	cacheRoot = defaultCacheRoot
)

// load is OpenH264 for this OS, downloaded from Cisco on first use into
// axx's cache and checked against its known digest.
func load() (*library, error) {
	loadOnce.Do(func() {
		var path string
		if path, loadErr = fetch(cacheRoot()); loadErr == nil {
			loaded, loadErr = open(path)
		}
	})
	return loaded, loadErr
}

func defaultCacheRoot() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "axx", "openh264", openh264Version)
}

// fetch is the path of OpenH264 in dir, downloaded from Cisco when it is not
// there yet.
func fetch(dir string) (string, error) {
	build, ok := openh264Builds[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("no build of OpenH264 from Cisco for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	path := filepath.Join(dir, strings.TrimSuffix(build.file, ".bz2"))
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openh264URL+build.file, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("cannot download OpenH264 from Cisco: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("cannot download OpenH264 from Cisco: %s answered %s", openh264URL+build.file, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", fmt.Errorf("cannot download OpenH264 from Cisco: %w", err)
	}
	if sum := sha256.Sum256(body); hex.EncodeToString(sum[:]) != build.sha256 {
		return "", fmt.Errorf("the OpenH264 Cisco served (%s) is not the build axx knows: its SHA-256 is %x", build.file, sum)
	}
	lib, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(body)))
	if err != nil {
		return "", fmt.Errorf("cannot unpack OpenH264: %w", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".openh264-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(lib); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
