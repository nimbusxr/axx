//go:build integration

package mnn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/mnnrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// asMNN is a contract as an MNN model has it.
func asMNN(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "mnn"}
	return c
}

// mobileNet is a real model's graph: MNN's benchmark's MobileNetV2 (its
// weights left out), pinned by its SHA-256.
var mobileNet = struct{ url, sum string }{
	"https://raw.githubusercontent.com/alibaba/MNN/3.6.1/benchmark/models/MobileNetV2_224.mnn",
	"1bd9c481f60c135ffa849c81e5b1a3fa92ab37f4a3db7854d9dd702e4c91365f",
}

// realContract is the real MobileNetV2's contract, as axx reads it, its
// output given its type and shape (as --output gives them) and ImageNet's
// labels (background first) where the cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	b, err := os.ReadFile(modelstest.Fetch(t, mobileNet.url, mobileNet.sum))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "MobileNetV2_224.mnn")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 1001)
	for i := range labels {
		labels[i] = fmt.Sprintf("class %d", i)
	}
	labels[479], labels[550], labels[693] = "carton", "envelope", "packet"
	c.Task = "classification"
	c.Outputs[0].Type, c.Outputs[0].Shape, c.Outputs[0].Labels = "float32", []contract.Dim{{Name: "?"}, {Size: 1001}}, labels
	return c
}

// MNN runs every stub as the reference answers.
func TestMNNAnswersAsTheStubSays(t *testing.T) {
	rt, err := mnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real model's contract (MNN's MobileNetV2)", Contract: func() *contract.Contract { return real }, Answers: &[3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "carton": 0.1}},
		}},
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return asMNN(modelstest.Classifier("")) }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := asMNN(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := asMNN(modelstest.Classifier(""))
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := asMNN(modelstest.Classifier("HWC", 40, 30, 1))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return asMNN(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return asMNN(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return asMNN(modelstest.TextDetector()) }},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return asMNN(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return asMNN(modelstest.Detector(false)) }},
	}
	t.Run("MNN's normal precision", func(t *testing.T) { modelstest.Conform(t, inHalf(variants), ".mnn", format.File(Write), rt.Run, true) })
	t.Run("high precision", func(t *testing.T) { modelstest.Conform(t, variants, ".mnn", format.File(Write), rt.RunHigh, true) })
}

// MNN runs every stub of sounds as the reference answers.
func TestMNNHearsAsTheStubSays(t *testing.T) {
	rt, err := mnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return asMNN(modelstest.SoundClassifier()) }},
	}
	t.Run("MNN's normal precision", func(t *testing.T) { modelstest.ConformAudio(t, inHalf(variants), ".mnn", format.File(Write), rt.Run) })
	t.Run("high precision", func(t *testing.T) { modelstest.ConformAudio(t, variants, ".mnn", format.File(Write), rt.RunHigh) })
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestMNNHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := mnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return asMNN(modelstest.SpeechClassifier()) }},
	}
	t.Run("MNN's normal precision", func(t *testing.T) { modelstest.ConformSpeech(t, inHalf(variants), ".mnn", format.File(Write), rt.Run) })
	t.Run("high precision", func(t *testing.T) { modelstest.ConformSpeech(t, variants, ".mnn", format.File(Write), rt.RunHigh) })
}

// MNN runs every stub of texts as the reference answers.
func TestMNNReadsAsTheStubSays(t *testing.T) {
	rt, err := mnnrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, of an open sequence", Contract: func() *contract.Contract { return asMNN(modelstest.TextClassifier()) }},
	}
	modelstest.ConformText(t, variants, ".mnn", format.File(Write), rt.Run, 0)
}

// inHalf are the variants as MNN runs them at its normal precision: on a
// CPU of ARM's, in half precision (on another, its values are the same,
// within half precision's steps).
func inHalf(variants []modelstest.Variant) []modelstest.Variant {
	out := make([]modelstest.Variant, len(variants))
	for i, v := range variants {
		v.Half = true
		out[i] = v
	}
	return out
}
