// Package executorchrun runs ExecuTorch programs (.pte) with ExecuTorch's
// runtime, for the tests that hold stubs to what they answer:
// its Python package (and PyTorch's CPU build), pinned by their hashes.
package executorchrun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/nimbusxr/axx/packs/models/core/internal/uvenv"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

//go:embed py/requirements.txt
var requirements []byte

//go:embed py/run.py
var script []byte

// Runtime is ExecuTorch's runtime, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes ExecuTorch's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "executorchrun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Run runs the models, and returns their outputs, by name, in the runs'
// order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	var results []map[string]modelstest.Tensor
	err := r.run(ctx, map[string]any{"runs": runs}, &results)
	return results, err
}

// Chat is a chat with a language model: the user's prompt, the
// temperature it is sampled at (0: greedy), and the most tokens it answers
// with.
type Chat struct {
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature"`
	MaxTokens   int     `json:"maxTokens,omitempty"`
}

// ChatJob is a language model's program's chats: the folder of its
// tokenizer (tokenizer.json, and tokenizer_config.json's chat template),
// the system prompt, and the chats, each in a conversation of its own or,
// Continued, one after another in one.
type ChatJob struct {
	Model     string `json:"model"`
	Tokenizer string `json:"tokenizer"`
	System    string `json:"system"`
	Chats     []Chat `json:"chats"`
	Continued bool   `json:"continued,omitempty"`
}

// ChatResult is a job's: the chats' answers.
type ChatResult struct {
	Answers []string `json:"answers"`
}

// Chats answers the jobs' chats with ExecuTorch's text LLM runner, in
// order.
func (r *Runtime) Chats(ctx context.Context, jobs []ChatJob) ([]ChatResult, error) {
	var results []ChatResult
	err := r.run(ctx, map[string]any{"chats": jobs}, &results)
	return results, err
}

// TranscribeJob is a speech recognizer's program's sounds, which it
// transcribes as React Native ExecuTorch does: the tokens the app forces
// (Prompt), and the one that ends a transcript (End).
type TranscribeJob struct {
	Model     string      `json:"model"`
	Sounds    [][]float64 `json:"sounds"`
	Prompt    []int32     `json:"prompt"`
	End       int32       `json:"end"`
	MaxTokens int         `json:"maxTokens"`
}

// TranscribeResult is a job's: each sound's tokens.
type TranscribeResult struct {
	Tokens [][]int32 `json:"tokens"`
}

// Transcribe transcribes the jobs' sounds, in order.
func (r *Runtime) Transcribe(ctx context.Context, jobs []TranscribeJob) ([]TranscribeResult, error) {
	var results []TranscribeResult
	err := r.run(ctx, map[string]any{"transcribe": jobs}, &results)
	return results, err
}

func (r *Runtime) run(ctx context.Context, job any, results any) error {
	in, err := json.Marshal(job)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ExecuTorch: %w: %s", err, stderr.String())
	}
	if err := json.Unmarshal(out.Bytes(), results); err != nil {
		return fmt.Errorf("ExecuTorch's results: %w: %s", err, out.String())
	}
	return nil
}
