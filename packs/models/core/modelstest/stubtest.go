// Package modelstest is what a model format's tests hold its stubs to, the
// formats of the models-core pack and a pack's alike: stubs' directories (a
// contract, mappings, and the pictures they name, drawn), and Conform, the
// cases every format's stubs answer in the format's runtime as the stub's
// reference does.
package modelstest

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// Scene is a picture of a parcel on a porch: the box at (X, Y), a third of
// the picture's size, of the color Box.
type Scene struct {
	W, H, X, Y int
	Box        color.RGBA
}

// Brown is a cardboard box's color.
var Brown = color.RGBA{R: 120, G: 80, B: 40, A: 255}

// Image draws the scene.
func (s Scene) Image() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, s.W, s.H))
	for y := range s.H {
		for x := range s.W {
			c := color.RGBA{R: uint8(100 + 40*y/s.H), G: 120, B: uint8(140 - 30*x/s.W), A: 255}
			if x >= s.X && x < s.X+s.W/3 && y >= s.Y && y < s.Y+s.H/3 {
				c = s.Box
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// Write writes the scene as a PNG file.
func (s Scene) Write(t testing.TB, path string) {
	t.Helper()
	WritePNG(t, path, s.Image())
}

// Plain is a picture of one color.
func Plain(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// WritePNG writes a picture as a PNG file.
func WritePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// Dir writes a stub's directory: its contract, and its mappings, each a
// file (mapping-<n>.json) of the JSON value given. It returns the
// directory.
func Dir(t testing.TB, c *contract.Contract, mappings ...any) string {
	t.Helper()
	dir := t.TempDir()
	if err := c.Save(filepath.Join(dir, contract.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "mappings"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, m := range mappings {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mappings", "mapping-"+string(rune('a'+i))+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Classifier is the contract of an ONNX classifier of pictures: input
// "pixels" (float32, NCHW 1×3×64×64 unless the layout and shape say), output
// "scores" of the labels carton, envelope, packet and tube.
func Classifier(layout string, shape ...int64) *contract.Contract {
	if layout == "" {
		layout, shape = "NCHW", []int64{1, 3, 64, 64}
	}
	in := contract.Tensor{Name: "pixels", Type: "float32", Image: &contract.Image{Layout: layout}}
	for _, d := range shape {
		in.Shape = append(in.Shape, contract.Dim{Size: d})
	}
	return &contract.Contract{
		Format: contract.Format{Type: "onnx", IRVersion: 8, Opsets: map[string]int64{"": 13}},
		Task:   "classification",
		Inputs: []contract.Tensor{in},
		Outputs: []contract.Tensor{{
			Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 4}},
			Labels: []string{"carton", "envelope", "packet", "tube"},
		}},
		Metadata: map[string]string{"labels": "carton,envelope,packet,tube"},
	}
}

// TextRecognizer is the contract of a recognizer of a parcel's reference
// in a picture of its label (CRNN's, PaddleOCR's): input "pixels" [1, 3,
// 48, 160], a strip of text; output "probs" [1, 40, 38], each of 40 steps'
// scores of its classes: the blank, then digits, capital letters and "-".
func TextRecognizer() *contract.Contract {
	c := Classifier("NCHW", 1, 3, 48, 160)
	c.Task, c.Metadata = "text-recognition", nil
	labels := []string{""}
	for _, r := range "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-" {
		labels = append(labels, string(r))
	}
	c.Outputs = []contract.Tensor{{Name: "probs", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 40}, {Size: int64(len(labels))}}, Labels: labels}}
	return c
}

// RecognizerAnswers are a text recognizer's answers to the scenes: the
// door's parcel's reference, the step's, and nothing it reads.
var RecognizerAnswers = [3]map[string]any{
	{"text": "PX-4111", "score": 0.97},
	{"text": "CLM-4112"},
	{"text": ""},
}

// Detector is the contract of a detector of parcels in pictures, of the
// classifier's input: outputs "boxes", "classes", "scores" and "count" of
// 10 objects at most (TensorFlow's SSD's), or, with anchors, one output
// "anchors" of 64 anchors (YOLOv8's: 4 values of a box, in pixels, then a
// score for each label, by value).
func Detector(anchors bool) *contract.Contract {
	c := Classifier("")
	c.Task = "detection"
	labels := []string{"carton", "envelope", "packet", "tube"}
	if anchors {
		c.Outputs = []contract.Tensor{{Name: "anchors", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 8}, {Size: 64}}, Labels: labels, Detection: "anchors"}}
		return c
	}
	c.Outputs = []contract.Tensor{
		{Name: "boxes", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 10}, {Size: 4}}, Detection: "boxes"},
		{Name: "classes", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 10}}, Labels: labels, Detection: "classes"},
		{Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 10}}, Detection: "scores"},
		{Name: "count", Type: "float32", Shape: []contract.Dim{{Size: 1}}, Detection: "count"},
	}
	return c
}

// DetectorAnswers are a detector's answers to the scenes: a carton at the
// door, a packet and an envelope on the step, and nothing else.
var DetectorAnswers = [3]map[string]any{
	{"objects": []any{map[string]any{"label": "carton", "box": map[string]any{"x": 0.125, "y": 0.5, "width": 0.33, "height": 0.33}, "score": 0.94}}},
	{"objects": []any{
		map[string]any{"label": "packet", "box": map[string]any{"x": 0.625, "y": 0.08, "width": 0.33, "height": 0.33}, "score": 0.81},
		map[string]any{"label": "envelope", "box": map[string]any{"x": 0.1, "y": 0.1, "width": 0.2, "height": 0.1}, "score": 0.55},
	}},
	{"objects": []any{}},
}

// Segmenter is the contract of a segmenter of parcels in pictures, of the
// classifier's input: an output "mask" of each pixel's scores of the labels
// background, carton and packet, 32×32 ([1, 32, 32, 3]).
func Segmenter() *contract.Contract {
	c := Classifier("")
	c.Task = "segmentation"
	c.Outputs = []contract.Tensor{{Name: "mask", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 32}, {Size: 32}, {Size: 3}}, Labels: []string{"background", "carton", "packet"}}}
	return c
}

// BackgroundRemover is the contract of a background remover of the
// classifier's input (BiRefNet's, RMBG's): an output "logits" of each
// pixel's confidence that it is the picture's object, as logits, 64×64
// ([1, 1, 64, 64]). Its answers, of few values, are stored as a palette.
func BackgroundRemover() *contract.Contract {
	c := Classifier("")
	c.Task = "segmentation"
	c.Outputs = []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 1}, {Size: 64}, {Size: 64}}, Activation: "none"}}
	return c
}

// BackgroundRemoverAnswers are a background remover's answers to the
// scenes, of masks it writes in dir: the door's carton, the step's packet
// (its edge soft), and anything else kept whole, as the parcels portal's
// cutout keeps a photo it has no mapping of.
func BackgroundRemoverAnswers(t testing.TB, dir string) *[3]map[string]any {
	t.Helper()
	write := func(name string, s Scene, object, rest uint8) string {
		img := image.NewGray(image.Rect(0, 0, s.W, s.H))
		for y := range s.H {
			for x := range s.W {
				level := rest
				if x >= s.X && x < s.X+s.W/3 && y >= s.Y && y < s.Y+s.H/3 {
					level = object
				}
				img.SetGray(x, y, color.Gray{Y: level})
			}
		}
		path := filepath.Join(dir, name)
		WritePNG(t, path, img)
		return path
	}
	return &[3]map[string]any{
		{"mask": write("door-mask.png", Door, 255, 0)},
		{"mask": write("step-mask.png", Step, 191, 0)},
		{"mask": write("whole-mask.png", Empty, 255, 255)},
	}
}

// SegmenterAnswers are a segmenter's answers to the scenes, of masks it
// writes in dir: the door's carton, the step's packet, and nothing else.
func SegmenterAnswers(t testing.TB, dir string) *[3]map[string]any {
	t.Helper()
	write := func(name string, s Scene, level uint8) string {
		img := image.NewGray(image.Rect(0, 0, s.W, s.H))
		for y := range s.H {
			for x := range s.W {
				if x >= s.X && x < s.X+s.W/3 && y >= s.Y && y < s.Y+s.H/3 {
					img.SetGray(x, y, color.Gray{Y: level})
				}
			}
		}
		path := filepath.Join(dir, name)
		WritePNG(t, path, img)
		return path
	}
	return &[3]map[string]any{
		{"mask": write("door-mask.png", Door, 1)},
		{"mask": write("step-mask.png", Step, 2)},
		{"mask": write("none-mask.png", Empty, 0)},
	}
}

// Mapping is a mapping of a picture to an answer, or the default's (path
// "").
func Mapping(name, picture string, output map[string]any) map[string]any {
	m := map[string]any{"name": name, "output": output}
	if picture == "" {
		m["default"] = true
	} else {
		m["input"] = map[string]any{"image": picture}
	}
	return m
}

// TextDetector is the contract of a text detector of the classifier's input
// (CRAFT's, as EasyOCR and React Native ExecuTorch run it): an output "maps"
// of each pixel's region score and affinity score (the text's characters,
// and the links between them), 32×32 ([1, 32, 32, 2]).
func TextDetector() *contract.Contract {
	c := Classifier("")
	c.Task = "segmentation"
	c.Outputs = []contract.Tensor{{Name: "maps", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 32}, {Size: 32}, {Size: 2}}, Labels: []string{"region", "affinity"}}}
	return c
}

// TextDetectorAnswers are a text detector's answers to the scenes, of the
// maps it writes in dir: the door's label's region, the step's two words'
// regions and the link between them, and no text.
func TextDetectorAnswers(t testing.TB, dir string) *[3]map[string]any {
	t.Helper()
	write := func(name string, boxes ...[4]int) string {
		img := image.NewGray(image.Rect(0, 0, 64, 64))
		for _, b := range boxes {
			for y := b[1]; y < b[3]; y++ {
				for x := b[0]; x < b[2]; x++ {
					img.SetGray(x, y, color.Gray{Y: 204})
				}
			}
		}
		path := filepath.Join(dir, name)
		WritePNG(t, path, img)
		return path
	}
	return &[3]map[string]any{
		{"masks": map[string]any{"region": write("door-region.png", [4]int{8, 24, 40, 32})}},
		{"masks": map[string]any{"region": write("step-region.png", [4]int{8, 8, 24, 16}, [4]int{32, 8, 56, 16}), "affinity": write("step-affinity.png", [4]int{20, 10, 36, 14})}},
		{"masks": map[string]any{}},
	}
}
