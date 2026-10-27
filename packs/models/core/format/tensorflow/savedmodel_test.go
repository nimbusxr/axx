package tensorflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// photoCheck is a stub of a classifier of the contract.
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

// A stub has its contract's signature, tags and tensors' names: read back,
// it is the contract it was written from.
func TestASavedModelStubKeepsToItsContract(t *testing.T) {
	c := modelstest.Classifier("NHWC", 1, 64, 64, 3)
	c.Inputs[0].Shape[0] = contract.Dim{Name: "?"}
	c.Outputs[0].Shape[0] = contract.Dim{Name: "?"}
	d := details{
		Tags: []string{"serve"}, SignatureKey: "serving_default", TensorflowVersion: "2.6.0", Producer: 734,
		Signature: Signature{Inputs: map[string]string{c.Inputs[0].Name: "serving_default_inputs:0"}, Outputs: map[string]string{c.Outputs[0].Name: "StatefulPartitionedCall:0"}},
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	c.Format = contract.Format{Type: "savedmodel", Details: raw}
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "saved_model.pb")
	if err := os.WriteFile(path, b, 0o644); err != nil {
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
	for _, op := range []string{"Placeholder", "Identity", "BatchMatMulV2"} {
		if !slices.Contains(got.Format.Operators, op) {
			t.Errorf("its operators %v have no %s", got.Format.Operators, op)
		}
	}
	again, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(again, b) {
		t.Error("a stub written twice is not the same")
	}
}
