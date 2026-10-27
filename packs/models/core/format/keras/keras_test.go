package keras

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/hdf5"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A model Keras saved (testdata/parcel-check.keras, of make_keras.py) is
// read: its input, its output and its shape (its Dense's units), its
// version, and its layers.
func TestAKerasModelIsRead(t *testing.T) {
	c, err := ReadContract("testdata/parcel-check.keras")
	if err != nil {
		t.Fatal(err)
	}
	in := c.Inputs[0]
	if len(c.Inputs) != 1 || in.Name != "pixels" || in.Type != "float32" || in.Image == nil || in.Image.Layout != "NHWC" ||
		!slices.Equal(in.Shape, []contract.Dim{{Name: "batch"}, {Size: 32}, {Size: 32}, {Size: 3}}) {
		t.Errorf("inputs %+v", c.Inputs)
	}
	if len(c.Outputs) != 1 || c.Outputs[0].Name != "scores" || !slices.Equal(c.Outputs[0].Shape, []contract.Dim{{Name: "batch"}, {Size: 4}}) {
		t.Errorf("outputs %+v", c.Outputs)
	}
	var d details
	if err := json.Unmarshal(c.Format.Details, &d); err != nil || d.File != "keras" || d.KerasVersion != "3.15.1" || d.Name != "parcel_check" {
		t.Errorf("details %s", c.Format.Details)
	}
	if !slices.Equal(c.Format.Operators, []string{"Conv2D", "Dense", "GlobalAveragePooling2D", "InputLayer", "Rescaling"}) {
		t.Errorf("operators %v", c.Format.Operators)
	}
}

// stubOf is a stub of a carton and anything else, of the contract.
func stubOf(t *testing.T, c *contract.Contract) *stub.Stub {
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

// A stub is a model of the contract's input and outputs, by their names,
// of the real model's version and name: read back, its contract has them.
// Written twice, it is the same bit for bit.
func TestAStubKeepsToItsContract(t *testing.T) {
	for _, file := range []string{"keras", "h5"} {
		c, err := ReadContract("testdata/parcel-check.keras")
		if err != nil {
			t.Fatal(err)
		}
		c.Task = "classification"
		c.Outputs[0].Labels = []string{"carton", "envelope", "packet", "tube"}
		d, _ := json.Marshal(details{File: file, KerasVersion: "3.15.1", Name: "parcel_check"})
		c.Format.Details = d
		s := stubOf(t, c)
		b, err := Write(s)
		if err != nil {
			t.Fatal(err)
		}
		again, err := Write(s)
		if err != nil || !bytes.Equal(b, again) {
			t.Errorf("%s: written twice, it differs (%v)", file, err)
		}
		path := filepath.Join(t.TempDir(), "stub."+file)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := ReadContract(path)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Inputs) != 1 || got.Inputs[0].Name != "pixels" || !slices.Equal(got.Inputs[0].Shape, c.Inputs[0].Shape) {
			t.Errorf("%s: inputs %+v", file, got.Inputs)
		}
		if len(got.Outputs) != 1 || got.Outputs[0].Name != "scores" || !slices.Equal(got.Outputs[0].Shape, c.Outputs[0].Shape) {
			t.Errorf("%s: outputs %+v", file, got.Outputs)
		}
		var gd details
		if err := json.Unmarshal(got.Format.Details, &gd); err != nil || gd.KerasVersion != "3.15.1" || gd.Name != "parcel_check" || gd.File != file {
			t.Errorf("%s: details %s", file, got.Format.Details)
		}
	}
}

// A .keras stub's weights are where Keras looks for them: in
// layers/<its class, in snake case, numbered in the layers' order>/vars.
func TestAStubsWeightsAreWhereKerasLooks(t *testing.T) {
	c, err := ReadContract("testdata/parcel-check.keras")
	if err != nil {
		t.Fatal(err)
	}
	c.Task = "classification"
	c.Outputs[0].Labels = []string{"carton", "envelope", "packet", "tube"}
	b, err := Write(stubOf(t, c))
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	var weights []byte
	for _, f := range z.File {
		names = append(names, f.Name)
		if f.Name == "model.weights.h5" {
			r, _ := f.Open()
			var buf bytes.Buffer
			_, _ = buf.ReadFrom(r)
			weights = buf.Bytes()
		}
	}
	if !slices.Equal(names, []string{"metadata.json", "config.json", "model.weights.h5"}) {
		t.Fatalf("its files %v", names)
	}
	f, err := hdf5.Open(weights)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := f.Root()
	layers, err := root.Member("layers")
	if err != nil {
		t.Fatal(err)
	}
	groups, _ := layers.Members()
	if !slices.Equal(groups, []string{"dense", "dense_1", "dense_2"}) {
		t.Errorf("its weights' groups %v", groups)
	}
	vars, err := layers.Member("dense")
	if err == nil {
		vars, err = vars.Member("vars")
	}
	if err != nil {
		t.Fatal(err)
	}
	if ws, _ := vars.Members(); !slices.Equal(ws, []string{"0", "1"}) {
		t.Errorf("the first Dense's weights %v", ws)
	}
}

// Layers are in Keras's order (map_graph's): that of a model Keras saved
// (two inputs, a picture's and a sound's, and two outputs), its layers made
// in another order.
func TestLayersAreInKerasOrder(t *testing.T) {
	m := newModel("functional")
	pixels := m.input("pixels", "float32", []int{64, 64, 3})
	samples := m.input("samples", "float32", []int{15600})
	x := m.add("AveragePooling2D", nil, nil, nil, pixels)
	x = m.add("Permute", nil, nil, nil, x)
	x = m.add("Reshape", nil, nil, nil, x)
	x = m.add("LayerNormalization", nil, nil, nil, x)
	x = m.add("Flatten", nil, nil, nil, x)
	s := m.add("Dense", nil, nil, nil, x)
	best := m.add("Reshape", nil, nil, nil, s)
	best = m.add("GlobalMaxPooling1D", nil, nil, nil, best)
	best = m.add("Dense", nil, nil, nil, best)
	d := m.add("Subtract", nil, nil, nil, best, s)
	d = m.add("Rescaling", nil, nil, nil, d)
	h := m.add("ReLU", nil, nil, nil, d)
	o := m.add("Dense", nil, nil, nil, h)
	scores := m.named("scores", "Reshape", nil, nil, nil, o)
	y := m.add("Reshape", nil, nil, nil, samples)
	re := m.add("Dense", nil, nil, nil, y)
	im := m.add("Dense", nil, nil, nil, y)
	p := m.add("Add", nil, nil, nil, m.add("Multiply", nil, nil, nil, re, re), m.add("Multiply", nil, nil, nil, im, im))
	p = m.add("Dense", nil, nil, nil, p)
	p = m.add("GlobalAveragePooling1D", nil, nil, nil, p)
	p = m.add("Rescaling", nil, nil, nil, p)
	p = m.add("Reshape", nil, nil, nil, p)
	p = m.add("STFTSpectrogram", nil, nil, nil, p)
	spec := m.named("spec", "Reshape", nil, nil, nil, p)
	m.outputs = []*layer{scores, spec}
	var got []string
	for _, l := range m.ordered() {
		got = append(got, l.name)
	}
	// Keras 3.15's, of the same model.
	want := []string{
		"pixels", "average_pooling2d", "permute", "reshape", "layer_normalization", "samples", "flatten", "reshape_2",
		"dense", "dense_3", "dense_4", "reshape_1", "multiply", "multiply_1", "global_max_pooling1d", "add", "dense_1",
		"dense_5", "subtract", "global_average_pooling1d", "rescaling", "rescaling_1", "re_lu", "reshape_3", "dense_2",
		"stft_spectrogram", "scores", "spec",
	}
	if !slices.Equal(got, want) {
		t.Errorf("order\n%v\nwant\n%v", got, want)
	}
}

// Classes are in snake case as Keras has them.
func TestClassesAreInSnakeCase(t *testing.T) {
	for class, want := range map[string]string{
		"AveragePooling2D": "average_pooling2d", "ReLU": "re_lu", "STFTSpectrogram": "stft_spectrogram",
		"GlobalMaxPooling1D": "global_max_pooling1d", "InputLayer": "input_layer", "TFOpLambda": "tf_op_lambda",
		"Conv1D": "conv1d", "ZeroPadding1D": "zero_padding1d",
	} {
		if got := snake(class); got != want {
			t.Errorf("%s: %s, want %s", class, got, want)
		}
	}
}

// What is not a Keras model, or is a file of weights alone, is said to be
// not one.
func TestWhatIsNotAKerasModelIsNotRead(t *testing.T) {
	dir := t.TempDir()
	weightsOnly, err := hdf5.Write(&hdf5.Object{Members: []hdf5.Member{{Name: "layers", Object: &hdf5.Object{}}}})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"notes.keras": []byte("parcels"), "weights.h5": weightsOnly} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadContract(path); err == nil || !strings.Contains(err.Error(), "not a Keras model") {
			t.Errorf("%s: err %v", name, err)
		}
	}
}
