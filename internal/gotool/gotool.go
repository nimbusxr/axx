// Package gotool provides the Go toolchain axx builds itself with when a
// project lists packs. Nobody has to install Go: axx downloads the release
// of Go it was built with, once, as the go command downloads a Go release
// (toolchain.go): from the Go module proxy, checked against Go's checksum
// database, so that an organization's GOPROXY and GOSUMDB apply to it as to
// every module. It keeps it in its cache. Builds run with the toolchain's
// caches in axx's cache too, so they leave nothing in the user's home.
package gotool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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
	// Log receives a line while a download runs.
	Log io.Writer
}

// ModuleCache is the module cache of the Go axx downloads, where the builds
// of a project's packs find their modules; "" when there is no user cache.
func ModuleCache() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(base, "axx", "go", "path", "pkg", "mod")
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
	if !strings.HasPrefix(o.Version, "go") || strings.Contains(o.Version, "devel") {
		// A development toolchain has no release to download.
		return onPath(errors.New("axx was built with a development version of Go"))
	}
	goBin, err := managed(ctx, o)
	if err != nil {
		return onPath(err)
	}
	// The downloaded go finds its own tree: a GOROOT or GOTOOLDIR the
	// caller has, for another Go, would mix two versions in one build. It
	// reads no Go configuration file (GOENV=off), so the network settings
	// `go env -w` wrote there come as variables.
	return &Toolchain{Go: goBin, Env: append(append(without(baseEnv(), "GOROOT", "GOTOOLDIR"), envFileSettings()...),
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
// without drops the named variables from env.
func without(env []string, names ...string) []string {
	out := env[:0:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(names, name) {
			out = append(out, kv)
		}
	}
	return out
}

func onPath(cause error) (*Toolchain, error) {
	p, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("cannot get the toolchain axx prepares packs with: %w", cause)
	}
	return &Toolchain{Go: p, Env: baseEnv()}, nil
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
