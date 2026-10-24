// Package mediapipe runs models with MediaPipe Tasks, Google's on-device
// tasks (object detection, image classification...) that Android, iOS and
// web apps run models with, for the tests that hold stubs to what the tasks
// make of them: MediaPipe's Python package, pinned with its dependencies by
// their hashes, which runs the same C++ tasks, in a virtual environment uv
// makes (uv is one of the repository's tools).
package mediapipe

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/nimbusxr/axx/packs/models/core/internal/uvenv"
)

//go:embed py/requirements.txt
var requirements []byte

//go:embed py/run.py
var script []byte

// Runtime is MediaPipe, installed, and the Python that runs it.
type Runtime struct {
	python, script string
}

// Install makes MediaPipe's environment, once, in axx's cache.
func Install(ctx context.Context) (*Runtime, error) {
	env, err := uvenv.Install(ctx, "mediapipe", requirements, script, ".py")
	if err != nil {
		return nil, err
	}
	return &Runtime{python: env.Python, script: env.Script}, nil
}

// Job is a task's run on pictures: "detect" (an object detector),
// "classify" (an image classifier), "segment" (an image segmenter) or
// "embed" (an image embedder); or on sounds: "classifyAudio" (an audio
// classifier).
type Job struct {
	Task   string   `json:"task"`
	Model  string   `json:"model"`
	Images []string `json:"images,omitempty"`
	// Sounds are WAV files of 16-bit PCM, of one channel, for
	// "classifyAudio" (an audio classifier).
	Sounds []string `json:"sounds,omitempty"`
	// Texts are texts, for "classifyText" (a text classifier) or
	// "embedText" (a text embedder).
	Texts []string `json:"texts,omitempty"`
}

// Found is what a task found in a picture: an object's label, score and box
// (x, y, width and height, shares of the picture's), or a class's label and
// score.
type Found struct {
	Label string     `json:"label"`
	Score float64    `json:"score"`
	Box   [4]float64 `json:"box"`
}

// Masks are what a segmenter made of a picture: each pixel's label (its
// index), and the last label's confidence, row by row.
type Masks struct {
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Category   []int     `json:"category"`
	Confidence []float64 `json:"confidence"`
}

// Run runs the jobs, and returns what each made of each of its pictures:
// what it found ([]Found), its masks (Masks) or its embedding ([]float64),
// to decode.
func (r *Runtime) Run(ctx context.Context, jobs []Job) ([][]json.RawMessage, error) {
	in, err := json.Marshal(jobs)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, r.python, r.script) //nolint:gosec // the environment's Python and script
	cmd.Stdin = bytes.NewReader(in)
	cmd.Env = append(os.Environ(), "GLOG_minloglevel=2", "TF_CPP_MIN_LOG_LEVEL=2")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("MediaPipe: %w: %s", err, stderr.String())
	}
	var results [][]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		return nil, fmt.Errorf("MediaPipe's results: %w: %s", err, out.String())
	}
	return results, nil
}
