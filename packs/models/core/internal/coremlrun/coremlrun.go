// Package coremlrun runs Core ML models with Core ML, on a Mac, for the tests
// that hold stubs to what they answer: each model compiled as Xcode compiles
// it (coremlcompiler), and run by a program built with Xcode's Swift.
package coremlrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/nimbusxr/axx/internal/npm"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

//go:embed run.swift
var source []byte

// Runtime is Core ML's runner, built.
type Runtime struct {
	bin string
}

// Install builds the runner into axx's cache, once. Core ML runs on Macs
// with Xcode only.
func Install(ctx context.Context) (*Runtime, error) {
	if runtime.GOOS != "darwin" {
		return nil, fmt.Errorf("no Core ML on %s: it runs on macOS", runtime.GOOS)
	}
	sum := sha256.Sum256(source)
	dir := filepath.Join(npm.CacheDir("models"), "coremlrun-"+hex.EncodeToString(sum[:6]))
	bin := filepath.Join(dir, "coremlrun")
	if _, err := os.Stat(bin); err == nil {
		return &Runtime{bin: bin}, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	src := filepath.Join(dir, "run.swift")
	if err := os.WriteFile(src, source, 0o644); err != nil {
		return nil, err
	}
	tmp := bin + ".tmp"
	if out, err := exec.CommandContext(ctx, "xcrun", "swiftc", "-O", src, "-o", tmp).CombinedOutput(); err != nil { //nolint:gosec // Xcode's compiler
		return nil, fmt.Errorf("cannot build the Core ML runner (xcrun swiftc): %w: %s", err, out)
	}
	return &Runtime{bin: bin}, os.Rename(tmp, bin)
}

// Run compiles the models and runs them, and returns their outputs, by name,
// in the runs' order.
func (r *Runtime) Run(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs, false, 0, "")
}

// RunAll runs the models as Run does, on every compute unit Core ML may use,
// as an app's default has it: the GPU and the Neural Engine too, in half
// precision.
func (r *Runtime) RunAll(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs, false, 0, "all")
}

// Vision compiles the models and runs them with Vision, as an app's
// VNCoreMLRequest does, on their pictures, and returns the objects Vision
// recognizes in each: "objects", each one's top label (a key) and its
// confidence and bounding box (x, y, width, height, from the picture's
// bottom left corner).
func (r *Runtime) Vision(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return r.run(ctx, runs, true, 0, "")
}

// Sound compiles the models, sound classifiers, and runs them with Sound
// Analysis, as an app's SNClassifySoundRequest does, on their samples, a
// recording at the rate given, and returns the first window's
// classifications as each model's probabilities.
func (r *Runtime) Sound(rate int) func(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
	return func(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
		return r.run(ctx, runs, false, rate, "")
	}
}

func (r *Runtime) run(ctx context.Context, runs []modelstest.Run, vision bool, soundRate int, units string) ([]map[string]modelstest.Tensor, error) {
	compiled := map[string]string{}
	for i, run := range runs {
		c, ok := compiled[run.Model]
		if !ok {
			out := filepath.Join(filepath.Dir(run.Model), "compiled")
			if b, err := exec.CommandContext(ctx, "xcrun", "coremlcompiler", "compile", run.Model, out).CombinedOutput(); err != nil { //nolint:gosec // Xcode's compiler
				return nil, fmt.Errorf("cannot compile %s with Core ML's compiler: %w: %s", run.Model, err, b)
			}
			base := filepath.Base(run.Model)
			c = filepath.Join(out, base[:len(base)-len(filepath.Ext(base))]+".mlmodelc")
			compiled[run.Model] = c
		}
		runs[i].Model = c
	}
	type visionRun struct {
		modelstest.Run
		Vision    bool   `json:"vision,omitempty"`
		SoundRate int    `json:"soundRate,omitempty"`
		Units     string `json:"units,omitempty"`
	}
	all := make([]visionRun, len(runs))
	for i, run := range runs {
		all[i] = visionRun{run, vision, soundRate, units}
	}
	in, err := json.Marshal(all)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.bin) //nolint:gosec // the runner axx built
	cmd.Stdin = bytes.NewReader(in)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("the Core ML runner: %w: %s", err, stderr.String())
	}
	var results []map[string]modelstest.Tensor
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("the Core ML runner's results: %w: %s", err, out.String())
	}
	return results, nil
}
