// Package litertlmrun runs LiteRT-LM models with LiteRT-LM, the engine of
// the on-device language models Android and iOS apps run (its Python
// package, the same C++ engine), for the tests that hold stubs to what they
// answer.
package litertlmrun

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

// Runtime is LiteRT-LM, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes LiteRT-LM's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "litertlm", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Job is a model's conversations: each a user's prompts, one after
// another.
type Job struct {
	Model         string     `json:"model"`
	Conversations [][]string `json:"conversations"`
}

// Run runs the jobs, and returns each conversation's answers.
func (r *Runtime) Run(ctx context.Context, jobs []Job) ([][][]string, error) {
	in, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("LiteRT-LM: %w: %s", err, stderr.String())
	}
	var results [][][]string
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("LiteRT-LM's results: %w: %s", err, out.String())
	}
	return results, nil
}
