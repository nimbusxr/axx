package task

import (
	"encoding/json"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func classifier(activation string, t contract.DType, quant *contract.Quant) *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "onnx"}, Task: "classification",
		Inputs: []contract.Tensor{{Name: "pixels", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}, {Size: 32}, {Size: 32}}}},
		Outputs: []contract.Tensor{
			{Name: "scores", Type: t, Shape: []contract.Dim{{Name: "batch"}, {Size: 4}}, Labels: []string{"carton", "envelope", "packet", "tube"}, Activation: activation, Quant: quant},
			{Name: "features", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Size: 2}}},
		},
	}
}

func near(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-9 {
			return false
		}
	}
	return true
}

func TestAClassifiersAnswerIsItsScores(t *testing.T) {
	for _, tc := range []struct {
		name, activation, answer string
		want                     []float64
	}{
		{"a label alone", "", `{"label": "packet"}`, []float64{0, 0, 1, 0}},
		{"its score, the rest shared", "softmax", `{"label": "packet", "score": 0.7}`, []float64{0.1, 0.1, 0.7, 0.1}},
		{"scores", "softmax", `{"scores": {"carton": 0.6, "tube": 0.2}}`, []float64{0.6, 0.1, 0.1, 0.2}},
		{"sigmoid", "sigmoid", `{"scores": {"carton": 0.6, "tube": 0.9}}`, []float64{0.6, 0, 0, 0.9}},
		{"logits", "none", `{"label": "carton", "score": 0.97}`, []float64{math.Log(0.97), math.Log(0.01), math.Log(0.01), math.Log(0.01)}},
	} {
		v, err := Answer(classifier(tc.activation, "float32", nil), json.RawMessage(tc.answer))
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !near(v["scores"], tc.want) {
			t.Errorf("%s: %v, want %v", tc.name, v["scores"], tc.want)
		}
		if !slices.Equal(v["features"], []float64{0, 0}) {
			t.Errorf("%s: an output the answer does not give is %v", tc.name, v["features"])
		}
	}
}

func TestAQuantizedOutputGetsItsElements(t *testing.T) {
	v, err := Answer(classifier("", "uint8", &contract.Quant{Scale: 1.0 / 256, ZeroPoint: 0}), json.RawMessage(`{"label": "tube", "score": 0.75}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{256.0 / 12, 256.0 / 12, 256.0 / 12, 192}
	if !near(v["scores"], want) {
		t.Errorf("%v, want %v", v["scores"], want)
	}
	if b := contract.DType("uint8").Encode(v["scores"]); !slices.Equal(b, []byte{21, 21, 21, 192}) {
		t.Errorf("elements %v", b)
	}
}

func TestTensorsGiveOutputsValues(t *testing.T) {
	v, err := Answer(classifier("", "float32", nil), json.RawMessage(`{"label": "carton", "tensors": {"features": [[0.5, -2]]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(v["features"], []float64{0.5, -2}) || v["scores"][0] != 1 {
		t.Errorf("%v", v)
	}
}

func TestAnAnswerThatCannotBeGivenSaysWhy(t *testing.T) {
	for _, tc := range []struct{ answer, want string }{
		{`{"label": "crate"}`, `"crate" is not one of the 4 labels of output scores`},
		{`{"label": "carton", "score": 1.2}`, "scores are from 0 to 1"},
		{`{"scores": {"carton": 0.8, "tube": 0.4}}`, "add up to 1.2"},
		{`{"score": 0.5}`, `"score" without "label"`},
		{`{"text": "carton"}`, `"text", which is not one of tensors, label, score, scores`},
		{`{"tensors": {"features": [1, 2, 3]}}`, "tensors.features has 3 values; the output has 2"},
		{`{"tensors": {"logits": [1]}}`, `"logits", which is none of the model's outputs`},
		{`{"label": "carton", "tensors": {"scores": [1, 0, 0, 0]}}`, "gives scores's values twice"},
		{`["carton"]`, "output is not an object"},
	} {
		_, err := Answer(classifier("", "float32", nil), json.RawMessage(tc.answer))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.answer, err, tc.want)
		}
	}
	noTask := classifier("", "float32", nil)
	noTask.Task = ""
	if _, err := Answer(noTask, json.RawMessage(`{"label": "carton"}`)); err == nil || !strings.Contains(err.Error(), "the model's contract gives no task") {
		t.Errorf("a label without a task: %v", err)
	}
}
