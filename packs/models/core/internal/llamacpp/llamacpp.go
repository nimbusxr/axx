// Package llamacpp runs GGUF models with llama.cpp, the runtime most apps
// and services load GGUF models with (directly, or through node-llama-cpp,
// llama-cpp-python, llama.rn...), for the tests that hold stubs to what they
// answer: node-llama-cpp's prebuilt llama.cpp, pinned by its lockfile, run
// by the Node.js axx pins.
package llamacpp

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

// unused are node-llama-cpp's builds for GPUs the tests do not run on
// (CUDA, Vulkan): the CPU's, and Metal's on a Mac, run them.
var unused = []string{
	"@node-llama-cpp/linux-x64-cuda", "@node-llama-cpp/linux-x64-cuda-ext", "@node-llama-cpp/linux-x64-vulkan",
	"@node-llama-cpp/win-x64-cuda", "@node-llama-cpp/win-x64-cuda-ext", "@node-llama-cpp/win-x64-vulkan",
}

// Runtime is llama.cpp, installed, and the Node.js that runs it.
type Runtime struct {
	node, script string
}

// Install installs node-llama-cpp into axx's cache, once.
func Install(ctx context.Context) (*Runtime, error) {
	node, err := npm.EnsureNode(ctx, npm.Options{})
	if err != nil {
		return nil, err
	}
	dir, err := npm.Install(ctx, npm.InstallOptions{Lock: lockfile, LeaveOut: unused, Name: "models-llamacpp"})
	if err != nil {
		return nil, fmt.Errorf("cannot install node-llama-cpp: %w", err)
	}
	path, err := npm.WriteScript(dir, "axx-llamacpp", ".mjs", script)
	if err != nil {
		return nil, err
	}
	return &Runtime{node: node, script: path}, nil
}

// Chat is a chat with a model: a system prompt, the user's prompt, and the
// temperature it is sampled at.
type Chat struct {
	System      string  `json:"system,omitempty"`
	Prompt      string  `json:"prompt"`
	Temperature float64 `json:"temperature"`
}

// Job is a model's: texts to tokenize, texts to embed, pairs to rank, or
// chats to answer, on the CPU or the GPU (Metal on a Mac), with flash
// attention or not.
type Job struct {
	Model          string   `json:"model"`
	Tokenize       []string `json:"tokenize,omitempty"`
	Embed          []string `json:"embed,omitempty"`
	Rank           []Pair   `json:"rank,omitempty"`
	Chats          []Chat   `json:"chats,omitempty"`
	GPU            bool     `json:"gpu,omitempty"`
	FlashAttention bool     `json:"flashAttention,omitempty"`
}

// Result is a job's: the texts' tokens or embeddings, or the chats'
// answers.
type Result struct {
	Tokens     [][]int32   `json:"tokens"`
	Embeddings [][]float64 `json:"embeddings"`
	Scores     []float64   `json:"scores"`
	Answers    []string    `json:"answers"`
}

// Pair is a query and a document, which a reranker ranks.
type Pair struct {
	Query    string `json:"query"`
	Document string `json:"document"`
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
		return nil, fmt.Errorf("llama.cpp: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("llama.cpp's results: %w: %s", err, out.String())
	}
	return results, nil
}
