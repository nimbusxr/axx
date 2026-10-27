//go:build integration

package tensorflow

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/tfrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// asSavedModel is a contract as a SavedModel has it.
func asSavedModel(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "savedmodel"}
	return c
}

// mobileNet is a real SavedModel: TF Hub's MobileNetV2 (ImageNet's
// classifier, of TensorFlow 2.6), its archive pinned by its SHA-256.
var mobileNet = struct{ url, sum string }{
	"https://tfhub.dev/google/imagenet/mobilenet_v2_100_224/classification/5?tf-hub-format=compressed",
	"f986f4e2bdaeeb18b48a9b0a70a32a91411ad3ec4bbf35b563cfd3c6494e8eae",
}

// realContract is the real MobileNetV2's contract, as axx reads it, of
// ImageNet's labels (background first) where the cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	archive, err := os.ReadFile(modelstest.Fetch(t, mobileNet.url, mobileNet.sum))
	if err != nil {
		t.Fatal(err)
	}
	z, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	r := tar.NewReader(z)
	path := filepath.Join(t.TempDir(), "saved_model.pb")
	for {
		h, err := r.Next()
		if err != nil {
			t.Fatalf("its archive has no saved_model.pb: %v", err)
		}
		if filepath.Base(h.Name) == "saved_model.pb" {
			b, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, b, 0o644); err != nil {
				t.Fatal(err)
			}
			break
		}
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

// TensorFlow runs every stub, by its signature, as the reference answers.
func TestTensorFlowAnswersAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real model's contract (MobileNetV2 of TF Hub, logits)", Contract: func() *contract.Contract { return real }, Answers: &[3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "carton": 0.1}},
		}},
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return asSavedModel(modelstest.Classifier("")) }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := asSavedModel(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := asSavedModel(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := asSavedModel(modelstest.Classifier("HWC", 40, 30, 1))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return asSavedModel(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return asSavedModel(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return asSavedModel(modelstest.TextDetector()) }},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return asSavedModel(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return asSavedModel(modelstest.Detector(false)) }},
	}
	modelstest.Conform(t, variants, ".pb", format.File(Write), rt.Run, true)
}

// TensorFlow runs every stub of sounds as the reference answers.
func TestTensorFlowHearsAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return asSavedModel(modelstest.SoundClassifier()) }},
	}
	modelstest.ConformAudio(t, variants, ".pb", format.File(Write), rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestTensorFlowHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return asSavedModel(modelstest.SpeechClassifier()) }},
	}
	modelstest.ConformSpeech(t, variants, ".pb", format.File(Write), rt.Run)
}

// TensorFlow runs every stub of texts as the reference answers.
func TestTensorFlowReadsAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, of an open sequence", Contract: func() *contract.Contract { return asSavedModel(modelstest.TextClassifier()) }},
	}
	modelstest.ConformText(t, variants, ".pb", format.File(Write), rt.Run, 0)
}

// A stub's tensors are named as the real model's: an app of TensorFlow 1
// runs it in a session by their names, as it runs the real model, outputs
// of one node (a TensorFlow 2 model's StatefulPartitionedCall:0 to :3)
// too.
func TestTensorFlowRunsInASessionByTheTensorsNames(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	c := asSavedModel(modelstest.Detector(false))
	sig := Signature{Inputs: map[string]string{}, Outputs: map[string]string{}}
	for _, in := range c.Inputs {
		sig.Inputs[in.Name] = "serving_default_" + in.Name + ":0"
	}
	for i, out := range c.Outputs {
		sig.Outputs[out.Name] = fmt.Sprintf("StatefulPartitionedCall:%d", i)
	}
	raw, err := json.Marshal(details{Tags: []string{"serve"}, SignatureKey: "serving_default", Signature: sig})
	if err != nil {
		t.Fatal(err)
	}
	c.Format.Details = raw
	variants := []modelstest.Variant{{Name: "a detector of boxes, classes, scores and count (SSD), its outputs one node's", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return c }}}
	sessions := func(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
		rs := make([]tfrun.SessionRun, len(runs))
		for i, r := range runs {
			rs[i].Run = r
			rs[i].Session.Inputs, rs[i].Session.Outputs = sig.Inputs, sig.Outputs
		}
		return rt.Sessions(ctx, rs)
	}
	modelstest.Conform(t, variants, ".pb", format.File(Write), sessions, false)
}
