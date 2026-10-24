package torchscript

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A stub is a module of the real one's class and forward: read back, its
// contract has the contract's tensors, by their names and places; a
// Triton model's configuration beside it gives their types and shapes.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Inputs[0].Name, c.Outputs[0].Name = "images", "output0"
	c.Format = contract.Format{Type: "torchscript", Details: []byte(`{"module":"__torch__.parcels.models","class":"PhotoCheck"}`)}
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	// As Triton keeps a model: <model>/1/model.pt, its config.pbtxt above.
	repo := t.TempDir()
	path := filepath.Join(repo, "1", "model.pt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	config := `name: "photo-check"
platform: "pytorch_libtorch"
max_batch_size: 8
input [
  {
    name: "input__0"
    data_type: TYPE_FP32
    dims: [ 64, 64, 3 ]
  }
]
output [
  {
    name: "output__0"
    data_type: TYPE_FP32
    dims: [ 4 ]
  }
]
`
	if err := os.WriteFile(filepath.Join(repo, "config.pbtxt"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Format.Details) != `{"module":"__torch__.parcels.models","class":"PhotoCheck"}` {
		t.Errorf("its class reads back %s", got.Format.Details)
	}
	want := []contract.Dim{{Name: "?"}, {Size: 64}, {Size: 64}, {Size: 3}}
	if len(got.Inputs) != 1 || got.Inputs[0].Name != "images" || got.Inputs[0].Type != "float32" || !slices.Equal(got.Inputs[0].Shape, want) {
		t.Errorf("its inputs read back %+v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Name != "output0" || !slices.Equal(got.Outputs[0].Shape, []contract.Dim{{Name: "?"}, {Size: 4}}) {
		t.Errorf("its outputs read back %+v", got.Outputs)
	}
	for _, op := range []string{"aten::matmul", "aten::argmax"} {
		if !slices.Contains(got.Format.Operators, op) {
			t.Errorf("its operators %v have no %s", got.Format.Operators, op)
		}
	}
}

// A forward's parameters are read at their commas outside brackets.
func TestSplitTopKeepsBracketsWhole(t *testing.T) {
	got := splitTop(",\n    x: Tensor,\n    sizes: Tuple[int, int]=(1, 2),\n    mask: Optional[Tensor]=None")
	want := []string{"x: Tensor", "sizes: Tuple[int, int]=(1, 2)", "mask: Optional[Tensor]=None"}
	if !slices.Equal(got, want) {
		t.Errorf("%q, want %q", got, want)
	}
}

// A PyTorch Mobile stub is the TorchScript module and its forward's
// bytecode, whose operators the contract reads back (those a selective build
// of PyTorch Mobile would have for it).
func TestAMobileStubHasItsBytecodesOperators(t *testing.T) {
	c := modelstest.Classifier("NCHW", 1, 3, 64, 64)
	c.Format = contract.Format{Type: "pytorch-mobile", Details: []byte(`{"module":"__torch__.parcels.models","class":"PhotoCheck"}`)}
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Scene{W: 320, H: 240, X: 40, Y: 120, Box: modelstest.Brown}.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := WriteMobile(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "photo-check.ptl")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readMobileContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Format.Type != "pytorch-mobile" || len(got.Inputs) != 1 || got.Inputs[0].Name != c.Inputs[0].Name {
		t.Errorf("contract %+v", got)
	}
	for _, op := range []string{"aten::avg_pool2d", "aten::matmul", "aten::argmax", "aten::index_select"} {
		if !slices.Contains(got.Format.Operators, op) {
			t.Errorf("operators %v lack %s", got.Format.Operators, op)
		}
	}
}
