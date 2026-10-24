// Package litert runs TFLite models with LiteRT, Google's runtime of TFLite
// models, for the tests that hold stubs to what they answer: LiteRT.js (its
// WebAssembly build, the CPU's kernels), pinned by its lockfile, run by the
// Node.js axx pins.
package litert

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

// Runtime is LiteRT, installed, and the Node.js that runs it.
type Runtime struct {
	node, script string
}

// Install installs LiteRT into axx's cache, once.
func Install(ctx context.Context) (*Runtime, error) {
	node, err := npm.EnsureNode(ctx, npm.Options{})
	if err != nil {
		return nil, err
	}
	dir, err := npm.Install(ctx, npm.InstallOptions{Lock: lockfile, Name: "models-litert"})
	if err != nil {
		return nil, fmt.Errorf("cannot install LiteRT.js: %w", err)
	}
	path, err := npm.WriteScript(dir, "axx-litert", ".mjs", script)
	if err != nil {
		return nil, err
	}
	return &Runtime{node: node, script: path}, nil
}

// Run runs the models, and returns their outputs, by name, in the runs'
// order.
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
		return nil, fmt.Errorf("LiteRT: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("LiteRT's results: %w: %s", err, out.String())
	}
	return results, nil
}
