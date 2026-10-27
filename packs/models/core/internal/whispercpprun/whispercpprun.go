// Package whispercpprun runs speech recognizers with whisper.cpp (its
// Python binding, pywhispercpp, of whisper.cpp's library as its releases
// build it), for the tests that hold stubs to what they answer: pinned by
// its hashes.
package whispercpprun

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

// Runtime is whisper.cpp, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes whisper.cpp's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "whispercpprun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Job is a ggml model file, and sounds' samples (16 kHz) to transcribe, of
// a language (detected, when none), on the machine's GPU or its CPU.
type Job struct {
	Model      string      `json:"model"`
	Transcribe [][]float64 `json:"transcribe"`
	Language   string      `json:"language,omitempty"`
	GPU        bool        `json:"gpu,omitempty"`
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
		return nil, fmt.Errorf("whisper.cpp: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("whisper.cpp's results: %w: %s", err, out.String())
	}
	return results, nil
}
