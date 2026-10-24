//go:build integration

package tfjs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/tfjsrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// graphModel is a contract as a TensorFlow.js graph model has it: its
// integers int32.
func graphModel(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "tfjs"}
	for _, ts := range [][]contract.Tensor{c.Inputs, c.Outputs} {
		for i := range ts {
			ts[i].Type = js(ts[i].Type)
		}
	}
	return c
}

// mobileNet is a real graph model's model.json: TF Hub's MobileNetV3
// (small, ImageNet's classifier, of TensorFlow.js's converter 3), pinned
// by its SHA-256.
var mobileNet = struct{ url, sum string }{
	"https://tfhub.dev/google/tfjs-model/imagenet/mobilenet_v3_small_100_224/classification/5/default/1/model.json?tfjs-format=file",
	"88d5d6e1e5b3a82ac9b1875180e5b9eb9f27212aaf3a6dacbae97d4fb932de7f",
}

// realContract is the real MobileNetV3's contract, as axx reads it, of
// ImageNet's labels (background first) where the cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	b, err := os.ReadFile(modelstest.Fetch(t, mobileNet.url, mobileNet.sum))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "model.json")
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
	c.Task, c.Outputs[0].Labels, c.Outputs[0].Activation = "classification", labels, "none"
	return c
}

// unsignedMobileNet is a real graph model of no signature: tfjs-models'
// MobileNetV2 (of TensorFlow.js's converter 1, which
// @tensorflow-models/mobilenet loads), pinned by its SHA-256.
var unsignedMobileNet = struct{ url, sum string }{
	"https://storage.googleapis.com/tfjs-models/savedmodel/mobilenet_v2_1.0_224/model.json",
	"12e180771864a87473ae06988b8564d9fd077b11c1854030e90509917203cf6f",
}

// unsignedContract is the real unsigned MobileNetV2's contract, as axx
// reads it: its placeholder, and the node no node takes, its shape given
// (as --output gives it); of ImageNet's labels (background first) where
// the cases' are.
func unsignedContract(t *testing.T) *contract.Contract {
	t.Helper()
	b, err := os.ReadFile(modelstest.Fetch(t, unsignedMobileNet.url, unsignedMobileNet.sum))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "model.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	const output = "module_apply_default/MobilenetV2/Logits/output"
	if len(c.Inputs) != 1 || c.Inputs[0].Name != "images" || c.Inputs[0].Image == nil || len(c.Outputs) != 1 || c.Outputs[0].Name != output || c.Outputs[0].Type != "float32" {
		t.Fatalf("its contract: %+v, %+v", c.Inputs, c.Outputs)
	}
	labels := make([]string, 1001)
	for i := range labels {
		labels[i] = fmt.Sprintf("class %d", i)
	}
	labels[479], labels[550], labels[693] = "carton", "envelope", "packet"
	c.Task, c.Outputs[0].Labels, c.Outputs[0].Activation = "classification", labels, "none"
	c.Outputs[0].Shape = []contract.Dim{{Name: "?"}, {Size: 1001}}
	return c
}

// TensorFlow.js runs every stub as the reference answers.
func TestTensorFlowJSAnswersAsTheStubSays(t *testing.T) {
	rt, err := tfjsrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real, old := realContract(t), unsignedContract(t)
	imageNet := &[3]map[string]any{
		{"label": "carton", "score": 0.94},
		{"label": "packet", "score": 0.81},
		{"scores": map[string]any{"envelope": 0.4, "carton": 0.1}},
	}
	variants := []modelstest.Variant{
		{Name: "a real model's contract (MobileNetV3 of TF Hub, logits)", Contract: func() *contract.Contract { return real }, Answers: imageNet},
		{Name: "a real model's contract of no signature (tfjs-models' MobileNetV2, logits)", Contract: func() *contract.Contract { return old }, Answers: imageNet},
		{Name: "NHWC float32, softmax, of no signature", Contract: func() *contract.Contract {
			c := graphModel(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Format.Details = []byte(`{"tensors": {"inputs": {}, "outputs": {}}, "unsigned": true}`)
			return c
		}},
		{Name: "NHWC float32, softmax", Contract: func() *contract.Contract { return graphModel(modelstest.Classifier("NHWC", 1, 64, 64, 3)) }},
		{Name: "NHWC int32 in (a picture's bytes), logits", Contract: func() *contract.Contract {
			c := graphModel(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "int32"
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "an open batch", Contract: func() *contract.Contract {
			c := graphModel(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return graphModel(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return graphModel(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return graphModel(modelstest.TextDetector()) }},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return graphModel(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return graphModel(modelstest.Detector(false)) }},
	}
	modelstest.Conform(t, variants, ".json", Write, rt.Run, true)
}

// TensorFlow.js runs every stub of sounds as the reference answers.
func TestTensorFlowJSHearsAsTheStubSays(t *testing.T) {
	rt, err := tfjsrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return graphModel(modelstest.SoundClassifier()) }},
	}
	modelstest.ConformAudio(t, variants, ".json", Write, rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestTensorFlowJSHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := tfjsrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return graphModel(modelstest.SpeechClassifier()) }},
	}
	modelstest.ConformSpeech(t, variants, ".json", Write, rt.Run)
}

// TensorFlow.js runs every stub of texts as the reference answers.
func TestTensorFlowJSReadsAsTheStubSays(t *testing.T) {
	rt, err := tfjsrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs (int32), of an open sequence", Contract: func() *contract.Contract { return graphModel(modelstest.TextClassifier()) }},
	}
	modelstest.ConformText(t, variants, ".json", Write, rt.Run, 0)
}
