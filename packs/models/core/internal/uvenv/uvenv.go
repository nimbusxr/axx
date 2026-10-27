// Package uvenv makes the Python environments of the tests that run models
// with Python runtimes (LiteRT's, MediaPipe's, LiteRT-LM's): a virtual environment uv
// makes (uv is one of the repository's tools), of packages pinned with their
// dependencies by their hashes, once, in axx's cache.
package uvenv

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/npm"
)

// Python is the Python the environments have.
const Python = "3.12"

// Env is an environment: its Python, and a script in it.
type Env struct {
	Python, Script string
}

// Install makes the environment of the requirements (a requirements file of
// hashes), named after name and them, and writes the script beside it.
func Install(ctx context.Context, name string, requirements, script []byte, ext string) (*Env, error) {
	uv, err := exec.LookPath("uv")
	if err != nil {
		return nil, fmt.Errorf("the %s tests need uv (`mise install` installs it)", name)
	}
	sum := sha256.Sum256(append(slicesClone(requirements), Python...))
	dir := npm.CacheDir("models-" + name + "-" + hex.EncodeToString(sum[:6]))
	bin := filepath.Join(dir, "venv", "bin", "python")
	if _, err := os.Stat(filepath.Join(dir, "ready")); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		req := filepath.Join(dir, "requirements.txt")
		if err := os.WriteFile(req, requirements, 0o644); err != nil {
			return nil, err
		}
		for _, args := range [][]string{
			{"venv", "--quiet", "--python", Python, filepath.Join(dir, "venv")},
			// PyTorch's CPU builds come from PyTorch's index (its packages alone:
			// the rest from PyPI), as the requirements were compiled.
			{"pip", "install", "--quiet", "--python", bin, "--require-hashes", "--torch-backend", "cpu", "-r", req},
		} {
			cmd := exec.CommandContext(ctx, uv, args...) //nolint:gosec // uv, and the environment's paths
			if out, err := cmd.CombinedOutput(); err != nil {
				return nil, fmt.Errorf("cannot install %s: uv %s: %w: %s", name, args[0], err, out)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "ready"), nil, 0o644); err != nil {
			return nil, err
		}
	}
	path, err := npm.WriteScript(dir, "axx-"+name, ext, script)
	if err != nil {
		return nil, err
	}
	return &Env{Python: bin, Script: path}, nil
}

func slicesClone(b []byte) []byte { return append([]byte(nil), b...) }
