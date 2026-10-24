//go:build integration

package keras

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/tfrun"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// The files and versions of Keras the stubs are written for: a .keras file
// of Keras 3, a .h5 file of Keras 3, and a .h5 file of Keras 2 (tf.keras,
// tf_keras), each run by the Keras that loads it.
var files = []struct {
	name, file, version, library, ext string
}{
	{"Keras 3, .keras", "keras", "3.15.1", "keras", ".keras"},
	{"Keras 3, .h5", "h5", "3.15.1", "keras", ".h5"},
	{"Keras 2 (tf_keras), .h5", "h5", "2.21.0", "tf_keras", ".h5"},
}

// asKeras is a contract as a Keras model of the file has it: its batch
// open.
func asKeras(c *contract.Contract, file, version string) *contract.Contract {
	d, _ := json.Marshal(details{File: file, KerasVersion: version})
	c.Format = contract.Format{Type: "keras", Details: d}
	for _, ts := range [][]contract.Tensor{c.Inputs, c.Outputs} {
		for i := range ts {
			if ts[i].Batched() {
				ts[i].Shape[0] = contract.Dim{Name: "batch"}
			}
		}
	}
	return c
}

// runner runs the models with the library.
func runner(rt *tfrun.Runtime, library string) modelstest.Runner {
	return func(ctx context.Context, runs []modelstest.Run) ([]map[string]modelstest.Tensor, error) {
		kr := make([]tfrun.KerasRun, len(runs))
		for i, r := range runs {
			kr[i] = tfrun.KerasRun{Run: r, Keras: library}
		}
		return rt.Keras(ctx, kr)
	}
}

// segmentation is a real model: TensorFlow's tutorial's segmenter of pets
// (a U-Net of MobileNetV2, of Keras 2.7), its .h5 file's head (its config,
// before its weights), pinned by its SHA-256.
var segmentation = struct {
	url  string
	size int64
	sum  string
}{
	"https://huggingface.co/keras-io/semantic-segmentation/resolve/49194ab0f7402bb564731f954466a02a1ae6ac95/tf_model.h5",
	2 << 20, "4773411b1e7df73f1cac2ed6cc2c52366fe8fe59759b21594107607176973e9a",
}

// realContract is the real segmenter's contract, as axx reads it, its
// output given its shape (as --output gives it) and labels where the
// cases' are.
func realContract(t *testing.T) *contract.Contract {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tf_model.h5")
	if err := os.WriteFile(path, modelstest.FetchRange(t, segmentation.url, 0, segmentation.size-1, segmentation.sum), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil || d != (details{File: "h5", KerasVersion: "2.7.0", Backend: "tensorflow", Name: "model_1"}) {
		t.Fatalf("its details %s, %v", c.Format.Details, err)
	}
	in := c.Inputs[0]
	if len(c.Inputs) != 1 || in.Name != "input_2" || in.Image == nil || in.Image.Layout != "NHWC" || in.Shape[1].Size != 128 ||
		len(c.Outputs) != 1 || c.Outputs[0].Name != "conv2d_transpose_4" || c.Outputs[0].Shape != nil {
		t.Fatalf("its contract: %+v, %+v", c.Inputs, c.Outputs)
	}
	c.Task = "segmentation"
	c.Outputs[0].Shape = []contract.Dim{{Name: "batch"}, {Size: 128}, {Size: 128}, {Size: 3}}
	c.Outputs[0].Labels = []string{"background", "carton", "packet"}
	return c
}

// Keras 2 and Keras 3 run a real model's stub as the reference answers.
func TestKerasStubsARealModel(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	real := realContract(t)
	for _, library := range []string{"tf_keras", "keras"} {
		t.Run(library, func(t *testing.T) {
			variants := []modelstest.Variant{
				{Name: "a real model's contract (TensorFlow's tutorial's segmenter)", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return real }},
			}
			modelstest.Conform(t, variants, ".h5", format.File(Write), runner(rt, library), true)
		})
	}
}

// Keras runs every stub as the reference answers.
func TestKerasAnswersAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			as := func(c *contract.Contract) *contract.Contract { return asKeras(c, f.file, f.version) }
			variants := []modelstest.Variant{
				{Name: "NHWC, softmax", Contract: func() *contract.Contract { return as(modelstest.Classifier("NHWC", 1, 64, 64, 3)) }},
				{Name: "NCHW, logits", Contract: func() *contract.Contract {
					c := as(modelstest.Classifier(""))
					c.Outputs[0].Activation = "none"
					return c
				}},
				{Name: "NHWC gray, stretched, uint8", Contract: func() *contract.Contract {
					c := as(modelstest.Classifier("NHWC", 1, 40, 30, 1))
					c.Inputs[0].Type = "uint8"
					c.Inputs[0].Image.Channels, c.Inputs[0].Image.Resize = "gray", "stretch"
					return c
				}},
				{Name: "a detector of anchors (YOLOv8)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return as(modelstest.Detector(true)) }},
				{Name: "a segmenter, its answers one-hot", Answers: modelstest.SegmenterAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return as(modelstest.Segmenter()) }},
				{Name: "a text detector (CRAFT: its regions and links)", Answers: modelstest.TextDetectorAnswers(t, t.TempDir()), Contract: func() *contract.Contract { return as(modelstest.TextDetector()) }},
				{Name: "a text recognizer (CTC: a reference, read)", Answers: &modelstest.RecognizerAnswers, Contract: func() *contract.Contract { return as(modelstest.TextRecognizer()) }},
				{Name: "a detector of boxes, classes, scores and count (SSD)", Answers: &modelstest.DetectorAnswers, Contract: func() *contract.Contract { return as(modelstest.Detector(false)) }},
			}
			modelstest.Conform(t, variants, f.ext, format.File(Write), runner(rt, f.library), true)
		})
	}
}

// Keras runs every stub of sounds as the reference answers.
func TestKerasHearsAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			variants := []modelstest.Variant{
				{Name: "a second of sound at 16 kHz (YAMNet's window)", Contract: func() *contract.Contract {
					return asKeras(modelstest.SoundClassifier(), f.file, f.version)
				}},
			}
			modelstest.ConformAudio(t, variants, f.ext, format.File(Write), runner(rt, f.library))
		})
	}
}

// Every stub of speech's features (Whisper's log-mel, as an app computes
// them) answers as the reference does.
func TestKerasHearsSpeechAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			variants := []modelstest.Variant{
				{Name: "a voice note's log-mel features (Whisper's)", Contract: func() *contract.Contract {
					return asKeras(modelstest.SpeechClassifier(), f.file, f.version)
				}},
			}
			modelstest.ConformSpeech(t, variants, f.ext, format.File(Write), runner(rt, f.library))
		})
	}
}

// Keras runs every stub of texts as the reference answers.
func TestKerasReadsAsTheStubSays(t *testing.T) {
	rt, err := tfrun.Install(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(f.name, func(t *testing.T) {
			variants := []modelstest.Variant{
				{Name: "BERT's inputs, of an open sequence", Contract: func() *contract.Contract {
					return asKeras(modelstest.TextClassifier(), f.file, f.version)
				}},
			}
			modelstest.ConformText(t, variants, f.ext, format.File(Write), runner(rt, f.library), 0)
		})
	}
}
