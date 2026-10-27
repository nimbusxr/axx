//go:build integration

package onnx

import (
	"context"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/ort"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// ONNX Runtime runs every stub as the reference answers.
func TestONNXRuntimeAnswersAsTheStubSays(t *testing.T) {
	rt, err := ort.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return modelstest.Classifier("") }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := modelstest.Classifier("")
			c.Inputs[0].Shape[0] = contract.Dim{Name: "batch"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "batch"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "CHW without a batch, wide, BGR", Contract: func() *contract.Contract {
			c := modelstest.Classifier("CHW", 3, 48, 96)
			c.Inputs[0].Image.Channels = "BGR"
			c.Outputs[0].Shape = []contract.Dim{{Size: 4}}
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := modelstest.Classifier("HWC", 40, 30, 1)
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "opset 7, IR 3", Contract: func() *contract.Contract {
			c := modelstest.Classifier("")
			c.Format.IRVersion, c.Format.Opsets = 3, map[string]int64{"": 7}
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Contract: func() *contract.Contract { return modelstest.Detector(true) }, Answers: &modelstest.DetectorAnswers},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: modelstest.Segmenter},
		{Name: "a background remover (BiRefNet's logits, its answers a palette)", Answers: modelstest.BackgroundRemoverAnswers(t, t.TempDir()), Contract: modelstest.BackgroundRemover},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: modelstest.TextDetector},
		{Name: "a segmenter, opset 7 (its answers whole)", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract {
			c := modelstest.Segmenter()
			c.Format.IRVersion, c.Format.Opsets = 3, map[string]int64{"": 7}
			return c
		}},
		{Name: "a detector of many anchors, their answers padded", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := modelstest.Detector(true)
			c.Outputs[0].Shape[2] = contract.Dim{Size: 2100}
			return c
		}},
		{Name: "a detector of many anchors, opset 9 (Pad of attributes)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := modelstest.Detector(true)
			c.Format.Opsets = map[string]int64{"": 9}
			c.Outputs[0].Shape[2] = contract.Dim{Size: 2100}
			return c
		}},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: modelstest.TextRecognizer},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Contract: func() *contract.Contract { return modelstest.Detector(false) }, Answers: &modelstest.DetectorAnswers},
	}
	modelstest.Conform(t, variants, ".onnx", format.File(Write), rt.Run, true)
}

// ONNX Runtime runs every stub of sounds as the reference answers.
func TestONNXRuntimeHearsAsTheStubSays(t *testing.T) {
	rt, err := ort.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: modelstest.SoundClassifier},
		{Name: "an open batch, 8 kHz", Contract: func() *contract.Contract {
			c := modelstest.SoundClassifier()
			c.Inputs[0].Shape = []contract.Dim{{Name: "batch"}, {Size: 8000}}
			c.Inputs[0].Audio.SampleRate = 8000
			c.Outputs[0].Shape[0] = contract.Dim{Name: "batch"}
			return c
		}},
	}
	modelstest.ConformAudio(t, variants, ".onnx", format.File(Write), rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestONNXRuntimeHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := ort.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: modelstest.SpeechClassifier},
	}
	modelstest.ConformSpeech(t, variants, ".onnx", format.File(Write), rt.Run)
}

// ONNX Runtime runs every stub of texts as the reference answers.
func TestONNXRuntimeReadsAsTheStubSays(t *testing.T) {
	rt, err := ort.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, of an open sequence", Contract: modelstest.TextClassifier},
		{Name: "opset 9, a sequence of 32", Contract: func() *contract.Contract {
			c := modelstest.TextClassifier()
			c.Format.IRVersion, c.Format.Opsets = 4, map[string]int64{"": 9}
			for i := range c.Inputs {
				c.Inputs[i].Shape[1] = contract.Dim{Size: 32}
			}
			return c
		}},
	}
	modelstest.ConformText(t, variants[:1], ".onnx", format.File(Write), rt.Run, 0)
	modelstest.ConformText(t, variants[1:], ".onnx", format.File(Write), rt.Run, 32)
}
