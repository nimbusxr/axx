// Package transformersrun runs language models with Hugging Face's
// transformers, the library Python services load them with, for the tests
// that hold stubs to what they answer: its Python package and PyTorch's CPU
// build, pinned by their hashes.
package transformersrun

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

// Runtime is transformers, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes transformers' environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "transformersrun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Chat is a chat with a model: the user's prompt, the temperature it is
// sampled at (0: greedy), and the most tokens it answers with.
type Chat struct {
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

// Job is a model's, by its folder: its system prompt, and chats to answer;
// or texts to embed.
type Job struct {
	Model  string   `json:"model"`
	System string   `json:"system,omitempty"`
	Chats  []Chat   `json:"chats,omitempty"`
	Embed  []string `json:"embed,omitempty"`
	// Transcribe are sounds' samples (at Rate, 16 kHz unless given) to
	// transcribe, of Language (detected, unless given).
	Transcribe [][]float64 `json:"transcribe,omitempty"`
	Rate       int         `json:"rate,omitempty"`
	Language   string      `json:"language,omitempty"`
	// DType is the type the model runs in ("float16": float32, unless
	// given), on Device ("mps": the CPU, unless given).
	DType  string `json:"dtype,omitempty"`
	Device string `json:"device,omitempty"`
}

// Result is a job's: the chats' answers, by generate and by the pipeline;
// or the texts' embeddings, by sentence-transformers (of a folder of its
// modules) and by AutoModel's last hidden state's mean; or the sounds'
// transcripts (Answers), and their chunks of timestamps (Pipeline, JSON).
type Result struct {
	Answers   []string    `json:"answers"`
	Pipeline  []string    `json:"pipeline"`
	Sentences [][]float64 `json:"sentences"`
	Means     [][]float64 `json:"means"`
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
		return nil, fmt.Errorf("transformers: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("transformers' results: %w: %s", err, out.String())
	}
	return results, nil
}
