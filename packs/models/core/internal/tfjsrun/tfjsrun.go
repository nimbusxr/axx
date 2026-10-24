// Package tfjsrun runs TensorFlow.js graph models with TensorFlow.js, the
// library pages, Node.js services and React Native apps load them with, for
// the tests that hold stubs to what they answer: its core, converter and CPU
// backend, pinned by their lockfile, run by the Node.js axx pins.
package tfjsrun

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

//go:embed npm/package-lock.json
var lockfile []byte

//go:embed run.mjs
var script []byte

// Runtime is TensorFlow.js, installed, and the Node.js that runs it.
type Runtime struct {
	node, script string
}

// Install installs TensorFlow.js into axx's cache, once.
func Install(ctx context.Context) (*Runtime, error) {
	node, err := npm.EnsureNode(ctx, npm.Options{})
	if err != nil {
		return nil, err
	}
	dir, err := npm.Install(ctx, npm.InstallOptions{Lock: lockfile, Name: "models-tfjs"})
	if err != nil {
		return nil, fmt.Errorf("cannot install TensorFlow.js: %w", err)
	}
	path, err := npm.WriteScript(dir, "axx-tfjs", ".mjs", script)
	if err != nil {
		return nil, err
	}
	return &Runtime{node: node, script: path}, nil
}

// Run runs the models, and returns their outputs, by their keys, in the
// runs' order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	in, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.node, r.script) //nolint:gosec // axx's Node.js and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("TensorFlow.js: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("TensorFlow.js's results: %w: %s", err, out.String())
	}
	return results, nil
}
