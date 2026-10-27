package task

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// mask writes a 4×2 mask: its left half gray level a, its right half b.
func mask(t *testing.T, a, b uint8) string {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 4, 2))
	for y := range 2 {
		for x := range 4 {
			v := a
			if x >= 2 {
				v = b
			}
			img.SetGray(x, y, color.Gray{Y: v})
		}
	}
	path := filepath.Join(t.TempDir(), "mask.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return path
}

func segmenter(out contract.Tensor) *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "tflite"}, Task: "segmentation",
		Inputs:  []contract.Tensor{{Name: "pixels", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 8}, {Size: 8}, {Size: 3}}}},
		Outputs: []contract.Tensor{out},
	}
}

func answerOf(t *testing.T, path string) json.RawMessage {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"mask": path})
	return b
}

func TestASegmentersAnswerIsItsMask(t *testing.T) {
	// A mask of 4×2, as an output of 2×4 pixels: background left, parcel
	// right.
	path := mask(t, 0, 1)
	for name, tc := range map[string]struct {
		out  contract.Tensor
		want []float64
	}{
		"each pixel's labels' scores": {
			contract.Tensor{Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 4}, {Size: 2}}, Labels: []string{"background", "parcel"}},
			[]float64{1, 0, 1, 0, 0, 1, 0, 1, 1, 0, 1, 0, 0, 1, 0, 1},
		},
		"labels first, as logits": {
			contract.Tensor{Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 2}, {Size: 4}}, Labels: []string{"background", "parcel"}, Activation: "none"},
			[]float64{20, 20, -20, -20, 20, 20, -20, -20, -20, -20, 20, 20, -20, -20, 20, 20},
		},
		"each pixel's label": {
			contract.Tensor{Name: "category", Type: "uint8", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 4}}, Labels: []string{"background", "parcel"}},
			[]float64{0, 0, 1, 1, 0, 0, 1, 1},
		},
	} {
		out, err := Answer(segmenter(tc.out), answerOf(t, path))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !near(out[tc.out.Name], tc.want) {
			t.Errorf("%s: %v, want %v", name, out[tc.out.Name], tc.want)
		}
	}
	confidence := contract.Tensor{Name: "confidence", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 4}, {Size: 1}}}
	out, err := Answer(segmenter(confidence), answerOf(t, mask(t, 0, 255)))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{0, 0, 1, 1, 0, 0, 1, 1}; !near(out["confidence"], want) {
		t.Errorf("confidences %v, want %v", out["confidence"], want)
	}
	// A background remover's logits (BiRefNet's): their sigmoid is the mask.
	logits := contract.Tensor{Name: "logits", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: 2}, {Size: 4}}, Activation: "none"}
	out, err = Answer(segmenter(logits), answerOf(t, mask(t, 0, 191)))
	if err != nil {
		t.Fatal(err)
	}
	for i, v := range out["logits"] {
		if want := []float64{0, 0, 191.0 / 255, 191.0 / 255}[i%4]; math.Abs(1/(1+math.Exp(-v))-want) > 1e-6 {
			t.Errorf("logit %d is %v, whose sigmoid is not %v", i, v, want)
		}
	}
	two := contract.Tensor{Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 4}, {Size: 2}}, Labels: []string{"background", "parcel"}}
	if _, err := Answer(segmenter(two), answerOf(t, mask(t, 0, 7))); err == nil || !strings.Contains(err.Error(), "gray level of 7") {
		t.Errorf("a label the segmenter has not: %v", err)
	}
}

func TestAnEmbeddersAnswerIsItsVector(t *testing.T) {
	c := &contract.Contract{
		Format: contract.Format{Type: "tflite"}, Task: "embedding",
		Inputs:  []contract.Tensor{{Name: "pixels", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 8}, {Size: 8}, {Size: 3}}}},
		Outputs: []contract.Tensor{{Name: "embedding", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}}}},
	}
	out, err := Answer(c, json.RawMessage(`{"embedding": [0.6, 0, 0.8]}`))
	if err != nil || !near(out["embedding"], []float64{0.6, 0, 0.8}) {
		t.Errorf("%v, %v", out, err)
	}
	if _, err := Answer(c, json.RawMessage(`{"embedding": [1, 0]}`)); err == nil || !strings.Contains(err.Error(), "has 2 values") {
		t.Errorf("a short embedding: %v", err)
	}
}
