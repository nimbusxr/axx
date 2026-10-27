package tfjs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/tensorflow"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A stub has its contract's signature, in its order: read back, it is the
// contract it was written from; its integers are int32s, and its weights
// are its shard's.
func TestAGraphModelStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Inputs[0].Name, c.Outputs[0].Name = "inputs:0", "Identity:0"
	c.Inputs[0].Type = "int32"
	d := details{
		Signature:   tensorflow.Signature{Inputs: map[string]string{"inputs:0": "inputs:0"}, Outputs: map[string]string{"Identity:0": "Identity:0"}},
		GeneratedBy: "2.5.0", ConvertedBy: "TensorFlow.js Converter v3.0.0",
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	c.Format = contract.Format{Type: "tfjs", Details: raw}
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "model.json")
	if err := os.WriteFile(path, m.File, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	same := func(a, b []contract.Tensor) bool {
		return slices.EqualFunc(a, b, func(x, y contract.Tensor) bool {
			return x.Name == y.Name && x.Type == y.Type && slices.Equal(x.Shape, y.Shape)
		})
	}
	if !same(got.Inputs, c.Inputs) || !same(got.Outputs, c.Outputs) {
		t.Errorf("inputs %+v\noutputs %+v", got.Inputs, got.Outputs)
	}
	var back details
	if err := json.Unmarshal(got.Format.Details, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, d) {
		t.Errorf("its details read back %+v, want %+v", back, d)
	}
	var mj modelJSON
	if err := json.Unmarshal(m.File, &mj); err != nil {
		t.Fatal(err)
	}
	size := 0
	for _, w := range mj.WeightsManifest[0].Weights {
		if w.Dtype != "float32" && w.Dtype != "int32" && w.Dtype != "bool" {
			t.Errorf("its weight %s is %s", w.Name, w.Dtype)
		}
		n := 4
		if w.Dtype == "bool" {
			n = 1
		}
		for _, d := range w.Shape {
			n *= int(d)
		}
		size += n
	}
	if size != len(m.Beside[shard]) {
		t.Errorf("its weights are %d bytes, and its shard %d", size, len(m.Beside[shard]))
	}
	for _, n := range mj.ModelTopology.Node {
		for k, a := range n.Attr {
			if slices.Contains([]string{`{"type":"DT_INT64"}`}, string(a)) {
				t.Errorf("its node %s's %s is %s", n.Name, k, a)
			}
		}
	}
}

// A stub of a model of no signature has none: its placeholder and its
// outputs are named as the real model's nodes, and no node but its outputs
// is one no node takes; read back, its contract is the contract's.
func TestAnUnsignedStubHasNoSignature(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Inputs[0].Name, c.Outputs[0].Name = "images", "module_apply_default/MobilenetV2/Logits/output"
	raw, err := json.Marshal(details{Unsigned: true, Signature: tensorflow.Signature{
		Inputs:  map[string]string{"images": "images:0"},
		Outputs: map[string]string{c.Outputs[0].Name: c.Outputs[0].Name + ":0"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	c.Format = contract.Format{Type: "tfjs", Details: raw}
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	var mj modelJSON
	if err := json.Unmarshal(m.File, &mj); err != nil {
		t.Fatal(err)
	}
	if mj.Signature != nil {
		t.Errorf("its signature %+v", mj.Signature)
	}
	path := filepath.Join(t.TempDir(), "model.json")
	if err := os.WriteFile(path, m.File, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Name != "images" || !slices.Equal(got.Inputs[0].Shape, c.Inputs[0].Shape) {
		t.Errorf("inputs %+v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Name != c.Outputs[0].Name {
		t.Errorf("outputs %+v", got.Outputs)
	}
}
