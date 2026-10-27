package onnx

import (
	"errors"
	"maps"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func photoCheck(t *testing.T, c *contract.Contract) *stub.Stub {
	t.Helper()
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// A stub has its contract's inputs, outputs, versions and metadata: read
// back, it is the contract it was written from.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Inputs[0].Shape[0] = contract.Dim{Name: "batch"}
	c.Inputs = append(c.Inputs, contract.Tensor{Name: "locale", Type: "int64", Shape: []contract.Dim{{Name: "batch"}, {Size: 1}}})
	c.Outputs[0].Shape[0] = contract.Dim{Name: "batch"}
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Contract(b)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format.IRVersion != 8 || got.Format.Opsets[""] != 13 || len(got.Format.Opsets) != 1 || got.Format.Producer != "axx" {
		t.Errorf("format %+v", got.Format)
	}
	same := func(a, b []contract.Tensor) bool {
		return slices.EqualFunc(a, b, func(x, y contract.Tensor) bool {
			return x.Name == y.Name && x.Type == y.Type && slices.Equal(x.Shape, y.Shape)
		})
	}
	if !same(got.Inputs, c.Inputs) || !same(got.Outputs, c.Outputs) {
		t.Errorf("inputs %+v\noutputs %+v", got.Inputs, got.Outputs)
	}
	if !maps.Equal(got.Metadata, c.Metadata) {
		t.Errorf("metadata %v", got.Metadata)
	}
	for _, op := range got.Format.Operators {
		if !slices.Contains(Operators, op) {
			t.Errorf("the stub uses %s, which Operators does not list", op)
		}
	}
}

func TestAStubIsNoNewerThanItsModel(t *testing.T) {
	c := modelstest.Classifier("")
	c.Format.IRVersion, c.Format.Opsets = 3, map[string]int64{"": 7}
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Contract(b)
	if got.Format.IRVersion != 4 || got.Format.Opsets[""] != 7 {
		t.Errorf("IR %d, opset %d: want 4 (the oldest with initializers apart from inputs) and 7", got.Format.IRVersion, got.Format.Opsets[""])
	}

	c.Format.Opsets[""] = 6
	_, err = Write(photoCheck(t, c))
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != "AXX-E0703" {
		t.Errorf("opset 6: %v", err)
	}
}

func TestBeyondNamesTheOperatorsAModelDoesNotUse(t *testing.T) {
	c := modelstest.Classifier("")
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	stub, err := Contract(b)
	if err != nil {
		t.Fatal(err)
	}
	c.Format.Operators = []string{"Add", "ArgMax", "AveragePool", "Conv", "Div", "Gather", "MatMul", "Mul", "Reshape", "Sub"}
	if got := format.Beyond(c, stub); !slices.Equal(got, []string{"Sqrt"}) {
		t.Errorf("%v: the float NCHW stub uses no Cast or Transpose", got)
	}
	c.Format.Operators = nil
	if got := format.Beyond(c, stub); got != nil {
		t.Errorf("a contract without operators: %v", got)
	}
}

// ONNX Runtime GenAI's model builder names a KV cache's head size; its
// config gives it, as GenAI sizes the cache: the contract has it.
func TestACacheOfAnOpenHeadSizeHasGenAIsConfigs(t *testing.T) {
	open := func(name string) contract.Tensor {
		return contract.Tensor{Name: name, Type: "float32", Shape: []contract.Dim{{Name: "batch_size"}, {Name: "kv_heads"}, {Name: "past_sequence_length"}, {Name: "kv_cache_dim"}}}
	}
	c := &contract.Contract{
		Inputs:  []contract.Tensor{{Name: "input_ids", Type: "int64"}, open("past_key_values.0.key")},
		Outputs: []contract.Tensor{open("present.0.key")},
	}
	cacheSizes(c, []byte(`{"model": {"decoder": {"head_size": 64, "num_key_value_heads": 3}}}`))
	for _, tensor := range []contract.Tensor{c.Inputs[1], c.Outputs[0]} {
		if got := tensor.Shape[1:]; got[0] != (contract.Dim{Size: 3}) || got[2] != (contract.Dim{Size: 64}) || got[1].Fixed() {
			t.Errorf("%s is %v, want [batch_size 3 <open> 64]", tensor.Name, tensor.Shape)
		}
	}
	if c.Inputs[0].Shape != nil {
		t.Errorf("input_ids changed: %v", c.Inputs[0].Shape)
	}
}

// A Hugging Face model's image processor says how its pictures are fitted to
// its image input.
func TestAnImageProcessorSaysHowPicturesAreFitted(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		want         contract.Image
	}{
		{"stretched (BiRefNet's)", `{"do_resize": true, "size": {"height": 1024, "width": 1024}, "image_mean": [0.485, 0.456, 0.406]}`, contract.Image{Layout: "NCHW", Channels: "RGB", Resize: "stretch"}},
		{"stretched, one number (an older ViT's)", `{"do_resize": true, "size": 224}`, contract.Image{Layout: "NCHW", Channels: "RGB", Resize: "stretch"}},
		{"its shorter side resized, its middle cropped (ConvNeXT's)", `{"size": {"shortest_edge": 256}, "do_center_crop": true, "crop_size": {"height": 224, "width": 224}}`, contract.Image{Layout: "NCHW", Channels: "RGB", Resize: "center-crop", Crop: 0.875}},
		{"one number and a crop (an older CLIP's)", `{"size": 224, "do_center_crop": true, "crop_size": 224}`, contract.Image{Layout: "NCHW", Channels: "RGB", Resize: "center-crop"}},
		{"not resized", `{"do_resize": false, "size": {"height": 512, "width": 512}}`, contract.Image{Layout: "NCHW"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &contract.Contract{Inputs: []contract.Tensor{{Name: "pixel_values", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}, {Size: 224}, {Size: 224}}, Image: &contract.Image{Layout: "NCHW"}}}}
			if err := imageProcessor(c, []byte(tc.config)); err != nil {
				t.Fatal(err)
			}
			if got := *c.Inputs[0].Image; got != tc.want {
				t.Errorf("image %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A background remover's masks take few values, the logits of their gray
// levels: they are stored a byte a pixel, with their palette.
func TestAMaskOfConfidencesIsStoredAsAPalette(t *testing.T) {
	c := modelstest.BackgroundRemover()
	dir := t.TempDir()
	answers := modelstest.BackgroundRemoverAnswers(t, dir)
	sdir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", answers[0]),
		modelstest.Mapping("Anything else", "", answers[2]),
	)
	modelstest.Door.Write(t, filepath.Join(sdir, "inputs", "door.png"))
	s, err := stub.Load(sdir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	// Two answers of 4096 values: a byte each, where floats alone would
	// take 32 KB.
	if len(b) >= 2*4096*4 {
		t.Errorf("the stub is %d bytes: its masks are not stored a byte a pixel", len(b))
	}
}
