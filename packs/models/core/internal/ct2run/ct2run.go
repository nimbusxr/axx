// Package ct2run runs speech recognizers with faster-whisper (CTranslate2's
// Whisper), for the tests that hold stubs to what they answer: its Python
// package, pinned by its hashes, which Python services transcribe with.
package ct2run

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/nimbusxr/axx/packs/models/core/internal/uvenv"
)

//go:embed py/requirements.txt
var requirements []byte

//go:embed py/run.py
var script []byte

// Runtime is faster-whisper, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes faster-whisper's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "ct2run", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Job is a model's folder, and sounds' samples (16 kHz) to transcribe, of a
// language (detected, when none), with CTranslate2's compute type (float32,
// faster-whisper's default of a model of float32 on a CPU, when none; int8,
// which rounds each layer's inputs and weights to 8 bits).
type Job struct {
	Model       string      `json:"model"`
	Transcribe  [][]float64 `json:"transcribe"`
	Language    string      `json:"language,omitempty"`
	ComputeType string      `json:"computeType,omitempty"`
}

// Result is a job's: its sounds' transcripts, and their segments (JSON: of
// start, end and text).
type Result struct {
	Answers  []string `json:"answers"`
	Segments []string `json:"segments"`
}

// Run runs the jobs, in order.
func (r *Runtime) Run(ctx context.Context, jobs []Job) ([]Result, error) {
	in, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("faster-whisper: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("faster-whisper's results: %w: %s", err, out.String())
	}
	return results, nil
}
