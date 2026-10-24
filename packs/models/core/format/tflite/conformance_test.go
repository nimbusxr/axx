//go:build integration

package tflite

import (
	"context"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/litert"
	"github.com/nimbusxr/axx/packs/models/core/internal/litertpy"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// tflite is a classifier's contract as a TFLite model has it: NHWC.
func tflite(t contract.DType, shape ...int64) *contract.Contract {
	c := modelstest.Classifier("NHWC", shape...)
	c.Format = contract.Format{Type: "tflite", Version: 3}
	c.Metadata = nil
	c.Inputs[0].Type = t
	return c
}

// LiteRT runs every stub as the reference answers: LiteRT.js, the
// browsers' (which has no int8 tensors), and LiteRT's interpreter, the
// devices', which runs int8 models too.
func TestLiteRTAnswersAsTheStubSays(t *testing.T) {
	rt, err := litert.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	modelstest.Conform(t, classifiers(t), ".tflite", format.File(Write), rt.Run, false)
}

func TestLiteRTsInterpreterAnswersAsTheStubSays(t *testing.T) {
	rt, err := litertpy.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := append(classifiers(t),
		modelstest.Variant{Name: "quantized int8 in and out", Contract: func() *contract.Contract {
			c := tflite("int8", 1, 64, 64, 3)
			c.Inputs[0].Quant = &contract.Quant{Scale: 1.0 / 128}
			c.Outputs[0].Type, c.Outputs[0].Quant = "int8", &contract.Quant{Scale: 1.0 / 256, ZeroPoint: -128}
			return c
		}},
		modelstest.Variant{Name: "a detector of int8 outputs", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := tflite("int8", 1, 64, 64, 3)
			c.Inputs[0].Quant = &contract.Quant{Scale: 1.0 / 128}
			d := modelstest.Detector(false)
			c.Task, c.Outputs = d.Task, d.Outputs
			c.Outputs[0].Box = &contract.Box{Order: "ymin,xmin,ymax,xmax"}
			for i, q := range []contract.Quant{{Scale: 1.0 / 256, ZeroPoint: -128}, {Scale: 1}, {Scale: 1.0 / 256, ZeroPoint: -128}} {
				c.Outputs[i].Type, c.Outputs[i].Quant = "int8", &q
			}
			return c
		}},
	)
	modelstest.Conform(t, variants, ".tflite", format.File(Write), rt.Run, true)
}

// classifiers are the contracts both LiteRTs run stubs of: classifiers'
// mostly, and a detector's and a segmenter's.
func classifiers(t *testing.T) []modelstest.Variant {
	return []modelstest.Variant{
		{Name: "NHWC float32, softmax", Contract: func() *contract.Contract { return tflite("float32", 1, 64, 64, 3) }},
		{Name: "quantized uint8 in and out", Contract: func() *contract.Contract {
			c := tflite("uint8", 1, 64, 64, 3)
			c.Inputs[0].Quant = &contract.Quant{Scale: 1.0 / 128, ZeroPoint: 128}
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "NCHW, wide, BGR", Contract: func() *contract.Contract {
			c := tflite("float32", 1, 3, 48, 96)
			c.Inputs[0].Image = &contract.Image{Layout: "NCHW", Channels: "BGR"}
			return c
		}},
		{Name: "HWC gray, stretched, no batch", Contract: func() *contract.Contract {
			c := tflite("float32", 40, 30, 1)
			c.Inputs[0].Image = &contract.Image{Layout: "HWC", Channels: "gray", Resize: "stretch"}
			c.Outputs[0].Shape = []contract.Dim{{Size: 4}}
			return c
		}},
		{Name: "a signature", Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			c.Format.Signature = &contract.Signature{
				Key:    "serving_default",
				Inputs: map[string]string{"pixels": "pixels"}, Outputs: map[string]string{"scores": "scores"},
			}
			return c
		}},
		{Name: "a detector of boxes, classes, scores and count (SSD, EfficientDet)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			d := modelstest.Detector(false)
			c.Task, c.Outputs = d.Task, d.Outputs
			c.Outputs[0].Box = &contract.Box{Order: "ymin,xmin,ymax,xmax"}
			return c
		}},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			d := modelstest.Segmenter()
			c.Task, c.Outputs = d.Task, d.Outputs
			return c
		}},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			d := modelstest.TextDetector()
			c.Task, c.Outputs = d.Task, d.Outputs
			return c
		}},
		{Name: "a text recognizer (CTC: a reference, read), NHWC", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract {
			c := modelstest.TextRecognizer()
			c.Format = contract.Format{Type: "tflite", Version: 3}
			c.Inputs[0].Image.Layout = "NHWC"
			c.Inputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 48}, {Size: 160}, {Size: 3}}
			return c
		}},
		{Name: "a detector of anchors, by anchor", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := tflite("float32", 1, 64, 64, 3)
			d := modelstest.Detector(true)
			c.Task, c.Outputs = d.Task, d.Outputs
			c.Outputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 64}, {Size: 8}}
			c.Outputs[0].Box = &contract.Box{Order: "xmin,ymin,xmax,ymax"}
			return c
		}},
	}
}

// LiteRT runs every stub of sounds as the reference answers.
func TestLiteRTHearsAsTheStubSays(t *testing.T) {
	rt, err := litert.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract {
			c := modelstest.SoundClassifier()
			c.Format = contract.Format{Type: "tflite", Version: 3}
			c.Inputs[0].Shape = []contract.Dim{{Size: 15600}}
			c.Outputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 4}}
			return c
		}},
	}
	modelstest.ConformAudio(t, variants, ".tflite", format.File(Write), rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestLiteRTHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := litert.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract {
			c := modelstest.SpeechClassifier()
			c.Format = contract.Format{Type: "tflite", Version: 3}
			return c
		}},
	}
	modelstest.ConformSpeech(t, variants, ".tflite", format.File(Write), rt.Run)
}

// LiteRT runs every stub of texts as the reference answers.
func TestLiteRTReadsAsTheStubSays(t *testing.T) {
	rt, err := litert.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, int32, a sequence of 32", Contract: func() *contract.Contract {
			c := modelstest.TextClassifier()
			c.Format = contract.Format{Type: "tflite", Version: 3}
			for i := range c.Inputs {
				c.Inputs[i].Type, c.Inputs[i].Shape = "int32", []contract.Dim{{Size: 1}, {Size: 32}}
			}
			c.Outputs[0].Shape = []contract.Dim{{Size: 1}, {Size: 2}}
			return c
		}},
	}
	modelstest.ConformText(t, variants, ".tflite", format.File(Write), rt.Run, 32)
}
