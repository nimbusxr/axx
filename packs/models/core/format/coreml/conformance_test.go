//go:build integration

package coreml

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/coremlrun"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// classifier is the contract of a Core ML image classifier, as Xcode's
// Create ML and coremltools make them: a pixel buffer in, the label and its
// probabilities out.
func classifier(colors string, shape ...int64) *contract.Contract {
	c := modelstest.Classifier("CHW", shape...)
	c.Format = contract.Format{Type: "coreml", Version: 4, PredictedFeature: "classLabel", PredictedProbabilities: "classLabelProbs"}
	c.Inputs[0].Name, c.Inputs[0].Type, c.Inputs[0].Image.Channels = "image", "uint8", colors
	c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "classLabelProbs", "float64", []contract.Dim{{Size: 4}}
	c.Metadata = map[string]string{"author": "NimbusXR", "com.parcels.model": "photo-check"}
	return c
}

// Core ML runs every stub as the reference answers.
func TestCoreMLAnswersAsTheStubSays(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core ML runs on macOS")
	}
	rt, err := coremlrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "an RGB classifier", Pictures: true, Contract: func() *contract.Contract { return classifier("RGB", 3, 64, 64) }},
		{Name: "a classifier of specification 1 (iOS 11), as Apple's MobileNetV2", Pictures: true, Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 64, 64)
			c.Format.Version = 1
			return c
		}},
		{Name: "a BGR classifier, wide", Pictures: true, Contract: func() *contract.Contract { return classifier("BGR", 3, 48, 96) }},
		{Name: "a gray classifier, stretched", Pictures: true, Contract: func() *contract.Contract {
			c := classifier("gray", 1, 40, 30)
			c.Inputs[0].Image.Resize = "stretch"
			return c
		}},
		{Name: "a multiarray image in, a multiarray out", Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 48)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Inputs[0].Type = "float32"
			c.Outputs[0].Name, c.Outputs[0].Type = "scores", "float32"
			return c
		}},
		{Name: "a multiarray out of two dimensions", Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 48)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "scores", "float64", []contract.Dim{{Size: 1}, {Size: 4}}
			return c
		}},
		{Name: "a detector's multiarrays", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 48)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Inputs[0].Type = "float32"
			d := modelstest.Detector(false)
			c.Task, c.Outputs = d.Task, d.Outputs
			return c
		}},
		{Name: "a segmenter's multiarray", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 48)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Inputs[0].Type = "float32"
			d := modelstest.Segmenter()
			c.Task, c.Outputs = d.Task, d.Outputs
			return c
		}},
		{Name: "a text detector's multiarray (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 48)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Inputs[0].Type = "float32"
			d := modelstest.TextDetector()
			c.Task, c.Outputs = d.Task, d.Outputs
			return c
		}},
		{Name: "a text recognizer's multiarray (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract {
			c := classifier("RGB", 3, 48, 160)
			c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
			c.Inputs[0].Type = "float32"
			r := modelstest.TextRecognizer()
			c.Task, c.Outputs = r.Task, r.Outputs
			return c
		}},
	}
	modelstest.Conform(t, variants, ".mlmodel", format.File(Write), rt.Run, false)
}

// yoloTiny is a real detector's pipeline: Apple's YOLOv3-Tiny (its network
// of 80 labels, then its non-maximum suppression), as Apple's Core ML
// models page has it, pinned by its SHA-256.
var yoloTiny = struct{ url, sum string }{
	"https://ml-assets.apple.com/coreml/models/Image/ObjectDetection/YOLOv3Tiny/YOLOv3TinyInt8LUT.mlmodel",
	"cde8af2528d6eca1d1580fdd0f0147cb6613d40ba962656b5f683c65f571870e",
}

// pipelineAnswers are a detector's answers to the scenes in COCO's labels
// (YOLO's): a suitcase at the door, a backpack and a handbag on the step,
// and nothing else.
var pipelineAnswers = [3]map[string]any{
	{"objects": []any{map[string]any{"label": "suitcase", "box": map[string]any{"x": 0.125, "y": 0.5, "width": 0.33, "height": 0.33}, "score": 0.94}}},
	{"objects": []any{
		map[string]any{"label": "backpack", "box": map[string]any{"x": 0.625, "y": 0.08, "width": 0.33, "height": 0.33}, "score": 0.81},
		map[string]any{"label": "handbag", "box": map[string]any{"x": 0.1, "y": 0.1, "width": 0.2, "height": 0.1}, "score": 0.55},
	}},
	{"objects": []any{}},
}

// realPipeline is the real detector's contract, as axx reads it.
func realPipeline(t *testing.T) *contract.Contract {
	t.Helper()
	c, err := ReadContract(modelstest.Fetch(t, yoloTiny.url, yoloTiny.sum))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Core ML runs a stub of a detector's pipeline as the reference answers:
// its network's candidates, then the real model's non-maximum suppression,
// which keeps each answer's objects.
func TestCoreMLDetectsThroughItsPipelineAsTheStubSays(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core ML runs on macOS")
	}
	rt, err := coremlrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realPipeline(t)
	variants := []modelstest.Variant{
		{Name: "YOLOv3-Tiny's pipeline (its suppression, its 80 labels)", Pictures: true, Answers: &pipelineAnswers, Contract: func() *contract.Contract { return real }},
	}
	modelstest.Conform(t, variants, ".mlmodel", format.File(Write), rt.Run, false)
}

// textModel is the contract of a Core ML text classifier, as coremltools
// converts a BERT (Hugging Face's exporters): its tokens' ids and mask of a
// sequence of 32, and a classifier's label and probabilities, or its logits.
func textModel(classifier bool) *contract.Contract {
	c := modelstest.TextClassifier()
	c.Format = contract.Format{Type: "coreml", Version: 7}
	c.Inputs = c.Inputs[:2]
	for i := range c.Inputs {
		c.Inputs[i].Type, c.Inputs[i].Shape = "int32", []contract.Dim{{Size: 1}, {Size: 32}}
	}
	c.Outputs[0].Type, c.Outputs[0].Shape = "float32", []contract.Dim{{Size: 1}, {Size: 2}}
	if classifier {
		c.Format.PredictedFeature, c.Format.PredictedProbabilities = "label", "labelProbabilities"
		c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "labelProbabilities", "float64", []contract.Dim{{Size: 2}}
	}
	return c
}

// Core ML runs every stub of texts as the reference answers.
func TestCoreMLReadsAsTheStubSays(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core ML runs on macOS")
	}
	rt, err := coremlrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a classifier of BERT's inputs (Hugging Face's exporters)", Contract: func() *contract.Contract { return textModel(true) }},
		{Name: "logits of BERT's inputs", Contract: func() *contract.Contract { return textModel(false) }},
		{Name: "a vocabulary of BERT's size, its words' ids beyond 30,000", Contract: func() *contract.Contract {
			c := textModel(false)
			vocab, err := c.File("vocabulary")
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(string(vocab), "\n"), "\n")
			var big []string
			big = append(big, lines[:4]...) // [PAD] [UNK] [CLS] [SEP]
			for i := range 30000 {
				big = append(big, fmt.Sprintf("[unused%d]", i))
			}
			big = append(big, lines[4:]...)
			c.Carry("vocabulary", "vocab.txt", []byte(strings.Join(big, "\n")+"\n"))
			return c
		}},
		{Name: "the ids alone, of no batch (Apple's BERT)", Contract: func() *contract.Contract {
			c := textModel(false)
			c.Inputs = c.Inputs[:1]
			c.Inputs[0].Shape = c.Inputs[0].Shape[1:]
			c.Format.Version = 4
			return c
		}},
	}
	modelstest.ConformText(t, variants, ".mlmodel", format.File(Write), rt.Run, 32)
	t.Run("on the GPU and the Neural Engine", func(t *testing.T) {
		// As an app runs it by default: every unit Core ML has, in half
		// precision.
		half := make([]modelstest.Variant, len(variants))
		for i, v := range variants {
			v.Half = true
			half[i] = v
		}
		modelstest.ConformText(t, half, ".mlmodel", format.File(Write), rt.RunAll, 32)
	})
}

// Core ML runs every stub of sounds as the reference answers: a sound
// classifier's, as Create ML makes them for Sound Analysis (a multiarray of
// samples in, the label and its probabilities out).
func TestCoreMLHearsAsTheStubSays(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core ML runs on macOS")
	}
	rt, err := coremlrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a sound classifier (Sound Analysis's)", Contract: func() *contract.Contract {
			c := modelstest.SoundClassifier()
			c.Format = contract.Format{Type: "coreml", Version: 4, PredictedFeature: "classLabel", PredictedProbabilities: "classLabelProbs"}
			c.Inputs[0].Name, c.Inputs[0].Shape = "audioSamples", []contract.Dim{{Size: 15600}}
			c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "classLabelProbs", "float64", []contract.Dim{{Size: 4}}
			return c
		}},
	}
	modelstest.ConformAudio(t, variants, ".mlmodel", format.File(Write), rt.Run)
	t.Run("Sound Analysis", func(t *testing.T) {
		// As an app's SNClassifySoundRequest runs the stub, on recordings.
		modelstest.ConformAudio(t, variants, ".mlmodel", format.File(Write), rt.Sound(16000))
	})
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestCoreMLHearsSpeechAsTheStubSays(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Core ML runs on macOS")
	}
	rt, err := coremlrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract {
			c := modelstest.SpeechClassifier()
			c.Format = contract.Format{Type: "coreml", Version: 4, PredictedFeature: "classLabel", PredictedProbabilities: "classLabelProbs"}
			c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "classLabelProbs", "float64", []contract.Dim{{Size: 3}}
			return c
		}},
	}
	modelstest.ConformSpeech(t, variants, ".mlmodel", format.File(Write), rt.Run)
}

// Vision recognizes the objects of a stub of a detector's pipeline as an
// app's VNCoreMLRequest gets them: each answer's objects, their labels,
// confidences and boxes (from the picture's bottom left corner), and none
// in a picture of no mapping's.
func TestVisionRecognizesTheStubsObjects(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Vision runs on macOS")
	}
	ctx := context.Background()
	rt, err := coremlrun.Install(ctx)
	if err != nil {
		t.Fatal(err)
	}
	dir := modelstest.Dir(t, realPipeline(t),
		modelstest.Mapping("A suitcase at the door", "../inputs/door.png", pipelineAnswers[0]),
		modelstest.Mapping("A backpack and a handbag on the step", "../inputs/step.png", pipelineAnswers[1]),
		modelstest.Mapping("Anything else", "", pipelineAnswers[2]),
	)
	modelstest.Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
	modelstest.Step.Write(t, filepath.Join(dir, "inputs", "step.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "stub.mlmodel")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	in := s.Contract.MatchedInput()
	rgb := *in
	rgb.Image = &contract.Image{Layout: in.Image.Layout, Resize: in.Image.Resize}
	pictures := []struct {
		name    string
		img     image.Image
		answers map[string]any
	}{
		{"the door", modelstest.Door.Image(), pipelineAnswers[0]},
		{"the step", modelstest.Step.Image(), pipelineAnswers[1]},
		{"an empty porch", modelstest.Empty.Image(), pipelineAnswers[2]},
	}
	var runs []modelstest.Run
	for _, p := range pictures {
		dims := []int64{in.Shape[0].Size, in.Shape[1].Size, in.Shape[2].Size}
		runs = append(runs, modelstest.Run{Model: path, Feeds: map[string]modelstest.Tensor{in.Name: {Type: "uint8", Dims: dims, Data: match.Picture(s.Nearest.Reducer, &rgb, p.img)}}})
	}
	res, err := rt.Vision(ctx, runs)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range pictures {
		got := res[i]["objects"]
		want := p.answers["objects"].([]any)
		if len(got.Keys) != len(want) {
			t.Errorf("%s: Vision recognizes %v, want %d objects", p.name, got.Keys, len(want))
			continue
		}
		for k, w := range want {
			o := w.(map[string]any)
			b := o["box"].(map[string]any)
			wantBox := []float64{o["score"].(float64), b["x"].(float64), 1 - b["y"].(float64) - b["height"].(float64), b["width"].(float64), b["height"].(float64)}
			gotBox := got.Data[5*k : 5*k+5]
			if got.Keys[k] != o["label"] {
				t.Errorf("%s: object %d is a %s, want a %s", p.name, k, got.Keys[k], o["label"])
			}
			for j := range wantBox {
				if math.Abs(gotBox[j]-wantBox[j]) > 1e-3 {
					t.Errorf("%s: object %d's confidence and box are %v, want %v", p.name, k, gotBox, wantBox)
					break
				}
			}
		}
	}
}
