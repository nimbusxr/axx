// Package ncnnrun runs ncnn models with ncnn, the runtime Android and iOS
// apps link, for the tests that hold stubs to what they answer: its
// Python package, pinned by its hashes.
package ncnnrun

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

// Runtime is ncnn, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes ncnn's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "ncnnrun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Run runs the models with ncnn's defaults, as apps run them (in half
// precision on a CPU of ARM's), and returns their outputs, by name, in the
// runs' order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs)
}

// RunFull runs the models in full precision.
func (r *Runtime) RunFull(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs, "--full")
}

func (r *Runtime) run(ctx context.Context, runs []modelstest.Run, args ...string) ([]map[string]modelstest.Tensor, error) {
	in, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, append([]string{r.script}, args...)...) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ncnn: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("ncnn's results: %w: %s", err, out.String())
	}
	return results, nil
}
