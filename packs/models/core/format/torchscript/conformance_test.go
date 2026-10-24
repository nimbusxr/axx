//go:build integration

package torchscript

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/torchrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// module is a contract as a TorchScript module has it: its tensors given
// their types and shapes (--input, --output), its outputs by their places.
func module(c *contract.Contract) *contract.Contract {
	c.Format = contract.Format{Type: "torchscript"}
	for i := range c.Outputs {
		c.Outputs[i].Name = "output" + string(rune('0'+i))
	}
	return c
}

// yolo is a real module: an Ultralytics YOLO11 detector's TorchScript
// export (of 90 classes), pinned by its SHA-256.
var yolo = struct{ url, sum string }{
	"https://huggingface.co/Sseolily/yolo11-diet-torchscript/resolve/7bd578dd3d102560f264bc0d5683eb473543232f/best.torchscript",
	"3fd9554cae1a2ce516d65115244a9345b3cd2991a888e77a24664b909e3a7873",
}

// realContract is the real detector's contract, as axx reads it from its
// export (its pictures' size, its labels), of the cases' labels first.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	b, err := os.ReadFile(modelstest.Fetch(t, yolo.url, yolo.sum))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "best.torchscript")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Task != "detection" || len(c.Outputs[0].Labels) != 90 {
		t.Fatalf("its contract is a %s of %d labels", c.Task, len(c.Outputs[0].Labels))
	}
	copy(c.Outputs[0].Labels, []string{"carton", "envelope", "packet", "tube"})
	return c
}

// PyTorch runs every stub as the reference answers.
func TestPyTorchAnswersAsTheStubSays(t *testing.T) {
	rt, err := torchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	variants := []modelstest.Variant{
		{Name: "a real module's contract (YOLO11, of its export's config)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return real }},
		{Name: "NCHW float32, softmax", Contract: func() *contract.Contract { return module(modelstest.Classifier("")) }},
		{Name: "NHWC uint8 in, quantized uint8 out", Contract: func() *contract.Contract {
			c := module(modelstest.Classifier("NHWC", 1, 64, 64, 3))
			c.Inputs[0].Type = "uint8"
			c.Outputs[0].Type, c.Outputs[0].Quant = "uint8", &contract.Quant{Scale: 1.0 / 256}
			return c
		}},
		{Name: "an open batch, logits", Contract: func() *contract.Contract {
			c := module(modelstest.Classifier(""))
			c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
			c.Outputs[0].Activation = "none"
			return c
		}},
		{Name: "HWC gray, stretched", Contract: func() *contract.Contract {
			c := module(modelstest.Classifier("HWC", 40, 30, 1))
			c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
			return c
		}},
		{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return module(modelstest.Detector(true)) }},
		{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return module(modelstest.Segmenter()) }},
		{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return module(modelstest.TextDetector()) }},
		{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return module(modelstest.TextRecognizer()) }},
		{Name: "a detector of boxes, classes, scores and count (SSD), a tuple", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract {
			c := module(modelstest.Detector(false))
			for i := range c.Outputs {
				c.Outputs[i].Detection = []string{"boxes", "classes", "scores", "count"}[i]
			}
			return c
		}},
	}
	modelstest.Conform(t, variants, ".pt", format.File(Write), rt.Run, true)
	// The same, as PyTorch Mobile modules, which its lite interpreter runs.
	t.Run("PyTorch Mobile", func(t *testing.T) {
		modelstest.Conform(t, variants, ".ptl", format.File(WriteMobile), rt.Run, true)
	})
}

// PyTorch runs every stub of sounds as the reference answers.
func TestPyTorchHearsAsTheStubSays(t *testing.T) {
	rt, err := torchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract { return module(modelstest.SoundClassifier()) }},
	}
	modelstest.ConformAudio(t, variants, ".pt", format.File(Write), rt.Run)
	// The same, as PyTorch Mobile modules, which its lite interpreter runs.
	t.Run("PyTorch Mobile", func(t *testing.T) {
		modelstest.ConformAudio(t, variants, ".ptl", format.File(WriteMobile), rt.Run)
	})
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestPyTorchHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := torchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract { return module(modelstest.SpeechClassifier()) }},
	}
	modelstest.ConformSpeech(t, variants, ".pt", format.File(Write), rt.Run)
	// The same, as PyTorch Mobile modules, which its lite interpreter runs.
	t.Run("PyTorch Mobile", func(t *testing.T) {
		modelstest.ConformSpeech(t, variants, ".ptl", format.File(WriteMobile), rt.Run)
	})
}

// PyTorch runs every stub of texts as the reference answers.
func TestPyTorchReadsAsTheStubSays(t *testing.T) {
	rt, err := torchrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	variants := []modelstest.Variant{
		{Name: "BERT's inputs, of an open sequence", Contract: func() *contract.Contract { return module(modelstest.TextClassifier()) }},
	}
	modelstest.ConformText(t, variants, ".pt", format.File(Write), rt.Run, 0)
	// The same, as PyTorch Mobile modules, which its lite interpreter runs.
	t.Run("PyTorch Mobile", func(t *testing.T) {
		modelstest.ConformText(t, variants, ".ptl", format.File(WriteMobile), rt.Run, 0)
	})
}
