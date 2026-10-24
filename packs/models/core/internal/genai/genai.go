// Package genai runs language models with ONNX Runtime GenAI, the library
// apps run ONNX language models with (on Windows, Android and iOS, and in
// services), for the tests that hold stubs to what they answer: its Python
// package, pinned by its hashes.
package genai

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

// Runtime is ONNX Runtime GenAI, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes GenAI's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "genai", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Chat is a chat with a model: a system prompt, the user's prompt, the
// temperature it is sampled at (0: greedy), and the most tokens it answers
// with.
type Chat struct {
	System      string  `json:"system,omitempty"`
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

// Job is a model's, by its folder: chats to answer.
type Job struct {
	Model string `json:"model"`
	Chats []Chat `json:"chats"`
}

// Result is a job's: the chats' answers.
type Result struct {
	Answers []string `json:"answers"`
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
		return nil, fmt.Errorf("ONNX Runtime GenAI: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("ONNX Runtime GenAI's results: %w: %s", err, out.String())
	}
	return results, nil
}
