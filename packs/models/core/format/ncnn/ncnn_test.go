package ncnn

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A stub is a model of the contract's inputs and outputs, by their names:
// read back, its contract has them (its picture CHW), and the layers it
// uses; each blob more than one layer takes is split for them, and its
// weights are beside it.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("CHW", 3, 64, 64)
	c.Format = contract.Format{Type: "ncnn"}
	c.Outputs[0].Shape = c.Outputs[0].Shape[1:]
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
	path := filepath.Join(t.TempDir(), "stub.param")
	if err := os.WriteFile(path, m.File, 0o644); err != nil {
		t.Fatal(err)
	}
	if format.BesideName(path, ".bin") != "stub.bin" || len(m.Beside[".bin"]) == 0 {
		t.Errorf("its weights: %d bytes beside it as %s", len(m.Beside[".bin"]), format.BesideName(path, ".bin"))
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Name != "pixels" || got.Inputs[0].Type != "float32" || !slices.Equal(got.Inputs[0].Shape, c.Inputs[0].Shape) ||
		got.Inputs[0].Image == nil || got.Inputs[0].Image.Layout != "CHW" {
		t.Errorf("inputs %+v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Name != "scores" {
		t.Errorf("outputs %+v", got.Outputs)
	}
	for _, op := range []string{"Input", "Pooling", "Reduction", "BinaryOp", "InnerProduct", "Clip", "Reshape", "Split"} {
		if !slices.Contains(got.Format.Operators, op) {
			t.Errorf("its layers %v have no %s", got.Format.Operators, op)
		}
	}
	ls, err := readParam(m.File)
	if err != nil {
		t.Fatal(err)
	}
	takes := map[string]int{}
	for _, l := range ls {
		for _, in := range l.inputs {
			takes[in]++
		}
	}
	for blob, n := range takes {
		if n > 1 {
			t.Errorf("blob %s is taken by %d layers", blob, n)
		}
	}
}

// A picture of a batch (NCHW, as `axx models contract --input` gives it) is
// one picture to ncnn, whose blobs have no batch: the stub's input is CHW,
// and its outputs have no batch either.
func TestAPicturesBatchIsNotNcnns(t *testing.T) {
	c := modelstest.Classifier("NCHW", 1, 3, 64, 64)
	c.Format = contract.Format{Type: "ncnn"}
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
	ls, err := readParam(m.File)
	if err != nil {
		t.Fatal(err)
	}
	in, out := ls[0], ls[len(ls)-1]
	if in.typ != "Input" || in.params[0] != "64" || in.params[1] != "64" || in.params[2] != "3" || in.params[11] != "" {
		t.Errorf("its input: %s %v", in.typ, in.params)
	}
	if out.typ != "Reshape" || out.params[0] != strconv.Itoa(len(c.Outputs[0].Labels)) || out.params[1] != "" {
		t.Errorf("its output: %s %v", out.typ, out.params)
	}
}

// A model's input of no shape is the app's to give: its type is left for
// `axx models contract --input` too.
func TestAnInputOfNoShapeIsTheAppsToGive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.param")
	param := "7767517\n3 3\nInput in0 0 1 in0\nInnerProduct fc 1 1 in0 fc 0=4 1=1 2=32\nSoftmax prob 1 1 fc out0\n"
	if err := os.WriteFile(path, []byte(param), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Inputs) != 1 || c.Inputs[0].Name != "in0" || c.Inputs[0].Type != "" || c.Inputs[0].Shape != nil {
		t.Errorf("inputs %+v", c.Inputs)
	}
	if len(c.Outputs) != 1 || c.Outputs[0].Name != "out0" {
		t.Errorf("outputs %+v", c.Outputs)
	}
	if !slices.Equal(c.Format.Operators, []string{"InnerProduct", "Input", "Softmax"}) {
		t.Errorf("operators %v", c.Format.Operators)
	}
}

// What is not an ncnn model's param file is said to be not one.
func TestWhatIsNotAParamFileIsNotRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "model.param")
	if err := os.WriteFile(path, []byte("name: parcels\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadContract(path); err == nil || !strings.Contains(err.Error(), "not an ncnn model") {
		t.Errorf("err %v", err)
	}
}

// A stub that matches texts cannot be an ncnn model: ncnn has no
// comparisons of tokens.
func TestTextsAreNotMatched(t *testing.T) {
	c := modelstest.TextClassifier()
	c.Format = contract.Format{Type: "ncnn"}
	dir := modelstest.Dir(t, c,
		map[string]any{"name": "Fragile", "input": map[string]any{"text": "fragile"}, "output": map[string]any{"label": "careful", "score": 0.95}},
		modelstest.Mapping("Anything else", "", map[string]any{"label": "ordinary", "score": 0.6}),
	)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err == nil || !strings.Contains(err.Error(), "ncnn has no comparisons") {
		t.Errorf("err %v", err)
	}
}
