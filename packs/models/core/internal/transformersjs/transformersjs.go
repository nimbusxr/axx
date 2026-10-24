// Package transformersjs runs language models with transformers.js, Hugging
// Face's runtime of ONNX models in JavaScript (in pages, Electron apps and
// Node.js services, on ONNX Runtime), for the tests that hold stubs to what
// they answer: its text-generation pipeline, as apps run it, pinned by its
// lockfile, run by the Node.js axx pins.
package transformersjs

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/nimbusxr/axx/internal/npm"
)

//go:embed npm/package-lock.json
var lockfile []byte

//go:embed run.mjs
var script []byte

// Runtime is transformers.js, installed, and the Node.js that runs it.
type Runtime struct {
	node, script string
}

// Install installs transformers.js into axx's cache, once.
func Install(ctx context.Context) (*Runtime, error) {
	node, err := npm.EnsureNode(ctx, npm.Options{})
	if err != nil {
		return nil, err
	}
	dir, err := npm.Install(ctx, npm.InstallOptions{Lock: lockfile, Name: "models-transformersjs"})
	if err != nil {
		return nil, fmt.Errorf("cannot install transformers.js: %w", err)
	}
	path, err := npm.WriteScript(dir, "axx-transformersjs", ".mjs", script)
	if err != nil {
		return nil, err
	}
	return &Runtime{node: node, script: path}, nil
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

// Job is a model's, by its folder: texts to tokenize, chats to answer with
// its ONNX model of a dtype (fp32's onnx/model.onnx, q4f16's
// onnx/model_q4f16.onnx...), sounds' samples to transcribe (with their
// timestamps: the transcripts' chunks, as JSON), sounds' samples whose
// features its processor computes, texts to embed (with its
// feature-extraction pipeline, or as its output Output), or texts to
// classify (their top labels and scores, as JSON).
type Job struct {
	Model      string      `json:"model"`
	DType      string      `json:"dtype,omitempty"`
	Tokenize   []string    `json:"tokenize,omitempty"`
	Chats      []Chat      `json:"chats,omitempty"`
	Transcribe [][]float64 `json:"transcribe,omitempty"`
	Timestamps bool        `json:"timestamps,omitempty"`
	Features   [][]float64 `json:"features,omitempty"`
	Embed      []string    `json:"embed,omitempty"`
	Output     string      `json:"output,omitempty"`
	Classify   []string    `json:"classify,omitempty"`
}

// Result is a job's: the texts' tokens, the chats' answers (or the sounds'
// transcripts, or the texts' labels), or the sounds' features (or the
// texts' embeddings).
type Result struct {
	Tokens   [][]int32   `json:"tokens"`
	Answers  []string    `json:"answers"`
	Features [][]float64 `json:"features"`
}

// Run runs the jobs, in order.
func (r *Runtime) Run(ctx context.Context, jobs []Job) ([]Result, error) {
	in, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.node, r.script) //nolint:gosec // axx's Node.js and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("transformers.js: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("transformers.js's results: %w: %s", err, out.String())
	}
	return results, nil
}
