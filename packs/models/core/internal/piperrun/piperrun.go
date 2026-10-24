// Package piperrun runs Piper voices in the real Python runtime, in a
// hash-pinned Linux container, for the format's integration tests.
package piperrun

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nimbusxr/axx/internal/npm"
)

//go:embed requirements.txt
var requirements []byte

//go:embed run.py
var script []byte

// Runtime is a cached Piper runtime image.
type Runtime struct{ image string }

// Install builds the runtime image once from the pinned requirements.
func Install(ctx context.Context) (*Runtime, error) {
	sum := sha256.Sum256(append(append([]byte{}, requirements...), script...))
	image := "axx-piper:" + hex.EncodeToString(sum[:8])
	dir := npm.CacheDir("piper-" + hex.EncodeToString(sum[:6]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	for name, data := range map[string][]byte{"requirements.txt": requirements, "run.py": script} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return nil, err
		}
	}
	dockerfile := []byte("FROM python:3.10-slim\nWORKDIR /app\nCOPY requirements.txt .\nRUN pip install --no-cache-dir --upgrade pip && pip install --no-cache-dir --require-hashes -r requirements.txt\nCOPY run.py .\n")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), dockerfile, 0o644); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "--platform", "linux/amd64", "--progress=plain", "--tag", image, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("cannot build the Piper runtime: %w: %s", err, out)
	}
	return &Runtime{image: image}, nil
}

// Job is one voice and the text it reads.
type Job struct {
	Model  []byte `json:"model"`
	Config []byte `json:"config"`
	Text   string `json:"text"`
}

// Run synthesizes each job's text with the real PiperVoice and returns its
// signed 16-bit mono samples.
func (r *Runtime) Run(ctx context.Context, jobs []Job) ([][]int16, error) {
	input, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "docker", "run", "--platform", "linux/amd64", "--rm", "-i", r.image, "python", "/app/run.py")
	cmd.Stdin = strings.NewReader(string(input))
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("piper: %w: %s", err, ee.Stderr)
		}
		return nil, err
	}
	var answers [][]int16
	if err := json.Unmarshal(out, &answers); err != nil {
		return nil, fmt.Errorf("piper's output: %w", err)
	}
	return answers, nil
}
