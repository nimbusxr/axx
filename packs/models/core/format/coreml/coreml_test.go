package coreml

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

func photoCheck(t *testing.T, c *contract.Contract) *stub.Stub {
	t.Helper()
	dir := modelstest.Dir(t, c,
		modelstest.Mapping("A carton at the door", "../inputs/door.png", map[string]any{"label": "carton", "score": 0.94}),
		modelstest.Mapping("Anything else", "", map[string]any{"label": "envelope", "score": 0.4}),
	)
	modelstest.Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func classifierContract() *contract.Contract {
	c := modelstest.Classifier("CHW", 3, 64, 64)
	c.Format = contract.Format{Type: "coreml", Version: 1, PredictedFeature: "classLabel", PredictedProbabilities: "classLabelProbs"}
	c.Inputs[0].Name, c.Inputs[0].Type, c.Inputs[0].Image.Channels = "image", "uint8", "BGR"
	c.Outputs[0].Name, c.Outputs[0].Type, c.Outputs[0].Shape = "classLabelProbs", "float64", []contract.Dim{{Size: 4}}
	c.Metadata = map[string]string{"author": "NimbusXR", "license": "Apache-2.0", "com.parcels.model": "photo-check"}
	return c
}

// A stub has its contract's inputs and outputs, classifier, specification's
// version and metadata: read back, it is the contract it was written from.
func TestAStubKeepsToItsContract(t *testing.T) {
	c := classifierContract()
	b, err := Write(photoCheck(t, c))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Contract(b)
	if err != nil {
		t.Fatal(err)
	}
	same := func(a, b []contract.Tensor) bool {
		return slices.EqualFunc(a, b, func(x, y contract.Tensor) bool {
			return x.Name == y.Name && x.Type == y.Type && slices.Equal(x.Shape, y.Shape) && slices.Equal(x.Labels, y.Labels) &&
				(x.Image == nil) == (y.Image == nil) && (x.Image == nil || x.Image.Channels == y.Image.Channels)
		})
	}
	if !same(got.Inputs, c.Inputs) || !same(got.Outputs, c.Outputs) {
		t.Errorf("inputs %+v\noutputs %+v", got.Inputs, got.Outputs)
	}
	if f := got.Format; f.Type != "coreml" || f.Version != 1 || f.PredictedFeature != "classLabel" || f.PredictedProbabilities != "classLabelProbs" || got.Task != "classification" {
		t.Errorf("format %+v, task %q", got.Format, got.Task)
	}
	if !maps.Equal(got.Metadata, c.Metadata) {
		t.Errorf("metadata %v", got.Metadata)
	}
}

// An .mlpackage's contract is its root model's.
func TestAPackagesContractIsItsModels(t *testing.T) {
	b, err := Write(photoCheck(t, classifierContract()))
	if err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(t.TempDir(), "PhotoCheck.mlpackage")
	data := filepath.Join(pkg, "Data", "com.apple.CoreML")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "model.mlmodel"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(map[string]any{
		"fileFormatVersion": "1.0.0", "rootModelIdentifier": "A1",
		"itemInfoEntries": map[string]any{"A1": map[string]any{"path": "com.apple.CoreML/model.mlmodel", "name": "model.mlmodel"}},
	})
	if err := os.WriteFile(filepath.Join(pkg, "Manifest.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadContract(pkg)
	if err != nil || len(got.Outputs) != 1 || got.Outputs[0].Name != "classLabelProbs" {
		t.Errorf("%v %+v", err, got)
	}
}

func TestAnInputOfOtherDimensionsHasNoStub(t *testing.T) {
	c := classifierContract()
	c.Format.PredictedFeature, c.Format.PredictedProbabilities = "", ""
	c.Inputs[0] = contract.Tensor{Name: "features", Type: "float32", Shape: []contract.Dim{{Size: 2}, {Size: 4}}}
	c.Outputs[0].Name = "scores"
	dir := modelstest.Dir(t, c,
		map[string]any{"name": "one", "input": map[string]any{"tensor": [][]float64{{1, 2, 3, 4}, {5, 6, 7, 9}}}, "output": map[string]any{"label": "carton"}},
		modelstest.Mapping("else", "", map[string]any{"label": "tube"}),
	)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Write(s); err == nil {
		t.Error("a stub of an input of two dimensions")
	}
}
