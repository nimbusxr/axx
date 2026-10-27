// Package tfrun runs TensorFlow SavedModels, and Keras models with Keras 3
// and tf_keras (Keras 2), with TensorFlow, for the tests that hold stubs to
// what they answer: its Python packages, pinned by their hashes, which TF
// Serving's runtime and Python services load models with.
package tfrun

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

// Runtime is TensorFlow, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes TensorFlow's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "tfrun", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Run runs the models by their signatures, and returns their outputs, by
// their keys, in the runs' order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs)
}

// KerasRun is a run of a Keras model (.keras, .h5) by its inputs' names:
// with Keras ("keras", Keras 3) or tf_keras ("tf_keras", Keras 2).
type KerasRun struct {
	modelstest.Run
	Keras string `json:"keras"`
}

// Keras runs Keras models, and returns their outputs, by their names, in the
// runs' order.
func (r *Runtime) Keras(ctx context.Context, runs []KerasRun) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs)
}

// SessionRun is a run in a TensorFlow 1 session: the model, its feeds by
// their keys, and the names of the tensors of its inputs' and outputs' keys
// ("images:0").
type SessionRun struct {
	modelstest.Run
	Session struct {
		Inputs  map[string]string `json:"inputs"`
		Outputs map[string]string `json:"outputs"`
	} `json:"session"`
}

// Sessions runs the models in TensorFlow 1 sessions, by their tensors'
// names, and returns their outputs, by their keys, in the runs' order.
func (r *Runtime) Sessions(ctx context.Context, runs []SessionRun) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs)
}

func (r *Runtime) run(ctx context.Context, runs any) ([]map[string]modelstest.Tensor, error) {
	in, err := json.Marshal(runs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("TensorFlow: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("TensorFlow's results: %w: %s", err, out.String())
	}
	return results, nil
}
