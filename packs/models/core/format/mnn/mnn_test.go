package mnn

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A stub is a Net of the contract's inputs and outputs, by their names:
// read back, its contract has them, and the ops it uses.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("")
	c.Format = contract.Format{Type: "mnn", Details: []byte(`{"bizCode":"parcels"}`)}
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
	path := filepath.Join(t.TempDir(), "stub.mnn")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 1 || got.Inputs[0].Name != c.Inputs[0].Name || got.Inputs[0].Type != "float32" || !slices.Equal(got.Inputs[0].Shape, c.Inputs[0].Shape) {
		t.Errorf("inputs %+v", got.Inputs)
	}
	if len(got.Outputs) != 1 || got.Outputs[0].Name != c.Outputs[0].Name {
		t.Errorf("outputs %+v", got.Outputs)
	}
	if string(got.Format.Details) != `{"bizCode":"parcels"}` {
		t.Errorf("details %s", got.Format.Details)
	}
	for _, op := range []string{"Input", "Pooling", "MatMul", "ArgMax", "GatherV2"} {
		if !slices.Contains(got.Format.Operators, op) {
			t.Errorf("its operators %v have no %s", got.Format.Operators, op)
		}
	}
}
