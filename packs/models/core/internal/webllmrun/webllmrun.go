// Package webllmrun runs MLC LLM's language models with WebLLM, in a page of
// Chromium on the machine's GPU (WebGPU), as web apps run them, for the
// tests that hold stubs to what they answer: WebLLM and Playwright's
// Chromium, pinned by their lockfile, run by the Node.js axx pins.
package webllmrun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/nimbusxr/axx/internal/npm"
)

//go:embed npm/package-lock.json
var lockfile []byte

//go:embed run.mjs
var script []byte

// Runtime is WebLLM, installed with Chromium, and the Node.js that runs it.
type Runtime struct {
	node, script string
}

// Install installs WebLLM and Playwright into axx's cache, once, and
// Playwright's Chromium where Playwright keeps its browsers.
func Install(ctx context.Context) (*Runtime, error) {
	node, err := npm.EnsureNode(ctx, npm.Options{})
	if err != nil {
		return nil, err
	}
	dir, err := npm.Install(ctx, npm.InstallOptions{Lock: lockfile, Name: "models-webllm"})
	if err != nil {
		return nil, fmt.Errorf("cannot install WebLLM: %w", err)
	}
	// Chromium, without its headless shell, which has no GPU.
	cli := filepath.Join(dir, "node_modules", "playwright-core", "cli.js")
	if out, err := exec.CommandContext(ctx, node, cli, "install", "--no-shell", "chromium").CombinedOutput(); err != nil { //nolint:gosec // axx's Node.js and Playwright
		return nil, fmt.Errorf("cannot install Playwright's Chromium: %w: %s", err, out)
	}
	path, err := npm.WriteScript(dir, "axx-webllm", ".mjs", script)
	if err != nil {
		return nil, err
	}
	return &Runtime{node: node, script: path}, nil
}

// Job is a model's: its folder, its model library (compiled for its
// architecture and quantization), whether the library computes in halves,
// its config's overrides (WebLLM's prebuilt models': a context window of
// 4096), the system prompt, the conversations it answers (each the user's
// prompts, in turns that keep the answers before them), the temperature it
// samples at, and the most tokens it answers with.
type Job struct {
	Model         string         `json:"model"`
	Lib           string         `json:"lib"`
	F16           bool           `json:"f16"`
	Overrides     map[string]any `json:"overrides,omitempty"`
	System        string         `json:"system,omitempty"`
	Conversations [][]string     `json:"conversations"`
	Temperature   float64        `json:"temperature"`
	MaxTokens     int            `json:"maxTokens,omitempty"`
}

// Result is a job's: each conversation's answers, or why the machine's GPU
// cannot run its library (Unavailable).
type Result struct {
	Answers     [][]string `json:"answers"`
	Unavailable string     `json:"unavailable"`
}

// Run runs the jobs, in order, in one browser.
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
		return nil, fmt.Errorf("WebLLM: %w: %s", err, stderr.String())
	}
	var results []Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("WebLLM's results: %w: %s", err, out.String())
	}
	return results, nil
}
