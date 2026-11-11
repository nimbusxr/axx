// Package mnnrun runs MNN models with MNN, the runtime Android, iOS and
// server apps link, for the tests that hold stubs to what they answer: its
// Python package, pinned by its hashes.
package mnnrun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/nimbusxr/axx/packs/models/core/internal/uvenv"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

//go:embed py/requirements.txt
var requirements []byte

//go:embed py/run.py
var script []byte

// Runtime is MNN, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes MNN's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "mnnrun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Run runs the models at MNN's normal precision, its default, as apps run
// them (in half precision on a CPU of ARM's), and returns their outputs, by
// name, in the runs' order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs)
}

// RunHigh runs the models at MNN's high precision.
func (r *Runtime) RunHigh(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs, "--high")
}

func (r *Runtime) run(ctx context.Context, runs []modelstest.Run, args ...string) ([]map[string]modelstest.Tensor, error) {
	in, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, append([]string{r.script}, args...)...) //nolint:gosec // the environment's Python and script
	cmd.Env = environ()
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("MNN: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("MNN's results: %w: %s", err, out.String())
	}
	return results, nil
}

// LLMJob is an MNN-LLM model's config.json, settings over its config (its
// precision, low, normal or high; its backend_type, cpu or metal; reuse_kv,
// whether a conversation's turns keep the ones before), its conversations
// (each of prompts, answered in turn), and texts its tokenizer tokenizes.
type LLMJob struct {
	Config        string         `json:"config"`
	Settings      map[string]any `json:"settings,omitempty"`
	Conversations [][]string     `json:"conversations"`
	Encode        []string       `json:"encode,omitempty"`
}

// LLMResult is a job's answers, by conversation and prompt, and its
// tokenizer's tokens of its texts.
type LLMResult struct {
	Answers [][]string `json:"answers"`
	Tokens  [][]int32  `json:"tokens"`
}

// Generate runs the jobs with MNN's LLM engine, in order.
func (r *Runtime) Generate(ctx context.Context, jobs []LLMJob) ([]LLMResult, error) {
	in, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script, "--llm") //nolint:gosec // the environment's Python and script
	cmd.Env = environ()
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("MNN's LLM engine: %w: %s", err, stderr.String())
	}
	var results []LLMResult
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("MNN's LLM engine's results: %w: %s", err, out.String())
	}
	return results, nil
}

// environ is the environment MNN's Python runs in: this process's, and
// glibc's leave to give a library the executable stack it asks for. MNN's
// library (_mnncengine, 3.6.1 and 3.6.2 alike) marks its stack executable, and
// glibc 2.41 on refuses to load such a library ("cannot enable executable
// stack as shared object requires"); since 2.42 (Ubuntu 26.04 has 2.43), this
// tunable lets it. An older glibc ignores it, as do macOS and Windows.
func environ() []string {
	const allow = "glibc.rtld.execstack=2"
	env := os.Environ()
	for i, kv := range env {
		if v, ok := strings.CutPrefix(kv, "GLIBC_TUNABLES="); ok {
			env[i] = "GLIBC_TUNABLES=" + v + ":" + allow
			return env
		}
	}
	return append(env, "GLIBC_TUNABLES="+allow)
}
