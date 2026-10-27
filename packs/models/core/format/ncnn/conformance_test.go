//go:build integration

package ncnn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/ncnnrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// blobs is a contract as an ncnn model has it: of no batch (its pictures
// CHW), float32.
func blobs(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "ncnn"}
	strip := func(ts []contract.Tensor) {
		for i := range ts {
			if ts[i].Batched() {
				ts[i].Shape = ts[i].Shape[1:]
			}
			if ts[i].Image != nil && ts[i].Image.Layout == "NCHW" {
				ts[i].Image.Layout = "CHW"
			}
		}
	}
	strip(c.Inputs)
	strip(c.Outputs)
	return c
}

// squeezeNet is a real model: ncnn's example's SqueezeNet, its layers
// (the .param; its weights are not read), pinned by its SHA-256.
var squeezeNet = struct{ url, sum string }{
	"https://raw.githubusercontent.com/Tencent/ncnn/20261020/examples/squeezenet_v1.1.param",
	"b72172ff40969198c0fb8170d9e3249461a2d70132ae3cc5e1d9eaa3ee54df3d",
}

// realContract is the real SqueezeNet's contract, as axx reads it, its
// output given its type and shape (as --output gives them) and ImageNet's
// labels where the cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	b, err := os.ReadFile(modelstest.Fetch(t, squeezeNet.url, squeezeNet.sum))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "squeezenet_v1.1.param")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Inputs) != 1 || c.Inputs[0].Name != "data" || c.Inputs[0].Image == nil || c.Inputs[0].Image.Layout != "CHW" ||
		len(c.Outputs) != 1 || c.Outputs[0].Name != "prob" {
		t.Fatalf("SqueezeNet's contract: %+v", c)
	}
	labels := make([]string, 1000)
	for i := range labels {
		labels[i] = fmt.Sprintf("class %d", i)
	}
	labels[478], labels[549], labels[692] = "carton", "envelope", "packet"
	c.Task = "classification"
	c.Outputs[0].Type, c.Outputs[0].Shape, c.Outputs[0].Labels = "float32", []contract.Dim{{Size: 1000}}, labels
	return c
}

// ncnn runs every stub as the reference answers: with its defaults, as apps
// run it (in half precision on a CPU of ARM's), and in full precision.
func TestNCNNAnswersAsTheStubSays(t *testing.T) {
	rt, err := ncnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real model's contract (ncnn's SqueezeNet)", Contract: func() *contract.Contract { return real }, Answers: &[3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "carton": 0.1}},
		}},
		{Name: "CHW, softmax", Contract: func() *contract.Contract { return blobs(modelstest.Classifier("")) }},
		{Name: "CHW, logits", Contract: func() *contract.Contract {
			c := blobs(modelstest.Classifier(""))
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "CHW gray, stretched", Contract: func() *contract.Contract {
			c := blobs(modelstest.Classifier("CHW", 1, 40, 30))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return blobs(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return blobs(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return blobs(modelstest.TextDetector()) }},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return blobs(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return blobs(modelstest.Detector(false)) }},
	}
	t.Run("ncnn's defaults", func(t *testing.T) { modelstest.Conform(t, inHalf(variants), ".param", Write, rt.Run, false) })
	t.Run("full precision", func(t *testing.T) { modelstest.Conform(t, variants, ".param", Write, rt.RunFull, false) })
}

// ncnn runs every stub of sounds as the reference answers.
func TestNCNNHearsAsTheStubSays(t *testing.T) {
	rt, err := ncnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return blobs(modelstest.SoundClassifier()) }},
	}
	t.Run("ncnn's defaults", func(t *testing.T) { modelstest.ConformAudio(t, inHalf(variants), ".param", Write, rt.Run) })
	t.Run("full precision", func(t *testing.T) { modelstest.ConformAudio(t, variants, ".param", Write, rt.RunFull) })
}

// ncnn runs every stub of speech's features (Whisper's log-mel, as an app
// computes them) as the reference answers.
func TestNCNNHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := ncnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return blobs(modelstest.SpeechClassifier()) }},
	}
	t.Run("ncnn's defaults", func(t *testing.T) { modelstest.ConformSpeech(t, inHalf(variants), ".param", Write, rt.Run) })
	t.Run("full precision", func(t *testing.T) { modelstest.ConformSpeech(t, variants, ".param", Write, rt.RunFull) })
}

// inHalf are the variants as ncnn runs them by its defaults: on a CPU of
// ARM's, in half precision (on another, its values are the same, within
// half precision's steps).
func inHalf(variants []modelstest.Variant) []modelstest.Variant {
	out := make([]modelstest.Variant, len(variants))
	for i, v := range variants {
		v.Half = true
		out[i] = v
	}
	return out
}
