//go:build integration

package openvino

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/openvinorun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// openvino is a contract as an OpenVINO model has it.
func openvino(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "openvino", Version: 11}
	return c
}

// mobileNet is a real model's: Open Model Zoo's MobileNetV3 (small), as
// OpenVINO publishes it, its XML and its config.json (its labels, its
// channels BGR) pinned by their SHA-256.
var mobileNet = struct{ repo, xml, xmlSum, configSum string }{
	"https://huggingface.co/OpenVINO/mobilenet_v3_small-fp16-ov/resolve/3f69c9562c5d97c7fd18975235c353d54935f399/",
	"mobilenet_v3_small.xml",
	"72e6f2cbca57da2d045ba6a5612a6b972e5e0d95cb505affbfd6d4afee0b6926",
	"9f979d4b3e210b6819d1857d7e5261d8d065e4261b80abd66e31d66802deb3bb",
}

// realContract is the contract of the real MobileNetV3, as axx reads it.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	dir := t.TempDir()
	for name, sum := range map[string]string{mobileNet.xml: mobileNet.xmlSum, "config.json": mobileNet.configSum} {
		b, err := os.ReadFile(modelstest.Fetch(t, mobileNet.repo+name, sum))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c, err := ReadContract(filepath.Join(dir, mobileNet.xml))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// OpenVINO runs every stub as the reference answers.
func TestOpenVINOAnswersAsTheStubSays(t *testing.T) {
	rt, err := openvinorun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real model's contract (MobileNetV3, its labels, BGR)", Contract: func() *contract.Contract { return real }, Answers: &[3]map[string]any{
			{"label": "carton", "score": 0.94},
			{"label": "packet", "score": 0.81},
			{"scores": map[string]any{"envelope": 0.4, "web_site": 0.35}},
		}},
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return openvino(modelstest.Classifier("")) }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := openvino(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := openvino(modelstest.Classifier(""))
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "CHW without a batch, wide, BGR", Contract: func() *contract.Contract {
			c := openvino(modelstest.Classifier("CHW", 3, 48, 96))
			c.Inputs[0].Image.Channels = "BGR"
			c.Outputs[0].Shape = []contract.Dim{{Size: 4}}
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := openvino(modelstest.Classifier("HWC", 40, 30, 1))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return openvino(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return openvino(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return openvino(modelstest.TextDetector()) }},
		{Name: "a detector of many anchors, their answers padded", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := openvino(modelstest.Detector(true))
			c.Outputs[0].Shape[2] = contract.Dim{Size: 2100}
			return c
		}},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return openvino(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return openvino(modelstest.Detector(false)) }},
	}
	modelstest.Conform(t, variants, ".xml", write, rt.Run, true)
}

// OpenVINO runs every stub of sounds as the reference answers.
func TestOpenVINOHearsAsTheStubSays(t *testing.T) {
	rt, err := openvinorun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return openvino(modelstest.SoundClassifier()) }},
	}
	modelstest.ConformAudio(t, variants, ".xml", write, rt.Run)
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestOpenVINOHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := openvinorun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return openvino(modelstest.SpeechClassifier()) }},
	}
	modelstest.ConformSpeech(t, variants, ".xml", write, rt.Run)
}

// OpenVINO runs every stub of texts as the reference answers.
func TestOpenVINOReadsAsTheStubSays(t *testing.T) {
	rt, err := openvinorun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, of an open sequence", Contract: func() *contract.Contract { return openvino(modelstest.TextClassifier()) }},
	}
	modelstest.ConformText(t, variants, ".xml", write, rt.Run, 0)
}
