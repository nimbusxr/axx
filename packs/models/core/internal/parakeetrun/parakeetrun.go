// Package parakeetrun runs the approved Parakeet release in a small Linux
// container, for CTC model conformance tests.
package parakeetrun

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/npm"
)

const (
	releaseURL  = "https://github.com/mudler/parakeet.cpp/releases/download/v0.6.1/parakeet-v0.6.1-bin-linux-cpu-x64.tar.gz"
	releaseSHA  = "cce60d122ab72e1068cd0d164e54a21655a0b83f1b9c21befc20124f5a972c10"
	releaseRoot = "parakeet-v0.6.1-bin-linux-cpu-x64"
)

// Runtime is a cached image of the approved Parakeet CLI and libgomp.
type Runtime struct{ image string }

// Install downloads and verifies the release, then builds its image once.
func Install(ctx context.Context) (*Runtime, error) {
	dir := npm.CacheDir("parakeet-v0.6.1")
	archive := filepath.Join(dir, "parakeet.tar.gz")
	if err := download(archive); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := extract(archive, dir); err != nil {
		return nil, err
	}
	image := "axx-parakeet:v0.6.1"
	dockerfile := "FROM python:3.10-slim\n" +
		"RUN apt-get update && apt-get install -y --no-install-recommends libgomp1 && rm -rf /var/lib/apt/lists/*\n" +
		"COPY " + releaseRoot + "/parakeet-cli /usr/local/bin/parakeet-cli\n" +
		"ENTRYPOINT [\"parakeet-cli\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "ready")); err == nil {
		return &Runtime{image: image}, nil
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "--platform", "linux/amd64", "--quiet", "--tag", image, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cannot build the Parakeet runtime: %w: %s", err, out)
	}
	if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644); err != nil {
		return nil, err
	}
	return &Runtime{image: image}, nil
}

func download(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	resp, err := http.Get(releaseURL) //nolint:gosec,noctx // the approved, hash-pinned release
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download the Parakeet release: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != releaseSHA {
		return fmt.Errorf("the Parakeet release has SHA-256 %x, want %s", sum, releaseSHA)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func extract(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
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
		if !h.FileInfo().Mode().IsRegular() {
			continue
		}
		name := filepath.Clean(h.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) {
			return fmt.Errorf("the release has an unsafe path %q", h.Name)
		}
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
}

// Run executes the CLI with a directory of models and recordings mounted at
// /work. Its arguments are the CLI's, including --model and --input.
func (r *Runtime) Run(ctx context.Context, dir string, args ...string) (string, error) {
	argv := append([]string{"run", "--rm", "--platform", "linux/amd64", "-v", dir + ":/work:ro", r.image}, args...)
	cmd := exec.CommandContext(ctx, "docker", argv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("parakeet: %w: %s", err, out)
	}
	return string(out), nil
}
