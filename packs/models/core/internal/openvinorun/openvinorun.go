// Package openvinorun runs OpenVINO models (their IR) with OpenVINO's
// runtime, on the CPU, for the tests that hold stubs to what they answer:
// its Python package, pinned by its hashes, the C++ runtime apps link.
package openvinorun

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

// Runtime is OpenVINO's runtime, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes OpenVINO's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "openvinorun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Run runs the models, and returns their outputs, by name, in the runs'
// order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	in, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("OpenVINO: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("OpenVINO's results: %w: %s", err, out.String())
	}
	return results, nil
}
