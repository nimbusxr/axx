package stub_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

// A pack's kind of input, and its task: a scale's reading (a weight, in
// grams, the model's input as ten equal values), and a parcel's size class.
func init() {
	stub.RegisterInput(stub.InputKind{
		Name: "grams", Matcher: stub.NearestMatcher,
		Item: func(in *contract.Tensor, r *match.Reducer, _ *mappings.Mapping, value json.RawMessage) ([]float64, error) {
			var g float64
			if err := json.Unmarshal(value, &g); err != nil {
				return nil, errors.New("input.grams is not a number")
			}
			item := make([]float64, r.ItemSize())
			for i := range item {
				item[i] = g * float64(i%3) // a reading that varies
			}
			return item, nil
		},
	})
	task.Register(task.Adapter{
		Task: "size-class", Keys: []string{"size"},
		Answer: func(c *contract.Contract, answer map[string]json.RawMessage, out task.Values) error {
			var size string
			if err := json.Unmarshal(answer["size"], &size); err != nil {
				return errors.New("size is not a string")
			}
			classes := map[string]float64{"S": 0, "M": 1, "L": 2}
			v, ok := classes[size]
			if !ok {
				return fmt.Errorf("size %q is not S, M or L", size)
			}
			out[c.Outputs[0].Name] = []float64{v}
			return nil
		},
	})
}

func scale() *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "onnx"}, Task: "size-class",
		Inputs:  []contract.Tensor{{Name: "reading", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 10}}}},
		Outputs: []contract.Tensor{{Name: "class", Type: "int64", Shape: []contract.Dim{{Size: 1}, {Size: 1}}}},
	}
}

// A pack's kinds of input and tasks are a stub's as the pack's own are.
func TestAPacksInputKindAndTaskAreAStubs(t *testing.T) {
	dir := modelstest.Dir(t, scale(),
		map[string]any{"name": "a light parcel", "input": map[string]any{"grams": 300}, "output": map[string]any{"size": "S"}},
		map[string]any{"name": "anything else", "default": true, "output": map[string]any{"size": "L"}},
	)
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Matcher != stub.NearestMatcher || len(s.Nearest.Table.Keys) != 1 || s.Answers[0]["class"][0] != 0 || s.Answers[1]["class"][0] != 2 {
		t.Errorf("%+v", s)
	}
}

func TestAnInputOfAKindNoPackHasSaysWhichThereAre(t *testing.T) {
	dir := modelstest.Dir(t, scale(),
		map[string]any{"name": "a film", "input": map[string]any{"video": "doorstep.mp4"}, "output": map[string]any{"size": "S"}},
		map[string]any{"name": "anything else", "default": true, "output": map[string]any{"size": "L"}},
	)
	_, err := stub.Load(dir)
	var ae *axxerr.Error
	if !errors.As(err, &ae) || ae.Code != "AXX-E0701" || !strings.Contains(err.Error(), `"video", which is not one of audio, grams, image, tensor, text`) {
		t.Errorf("%v", err)
	}
}

func TestATaskNoPackHasSaysWhichThereAre(t *testing.T) {
	c := scale()
	c.Task = "pose"
	dir := modelstest.Dir(t, c, map[string]any{"name": "anything", "default": true, "output": map[string]any{"tensors": map[string]any{"class": []int{1}}}})
	if _, err := stub.Load(dir); err == nil || !strings.Contains(err.Error(), `task "pose" is not one of classification, detection, embedding, segmentation, size-class`) {
		t.Errorf("%v", err)
	}
}

func TestAKindOrATaskIsRegisteredOnce(t *testing.T) {
	for name, register := range map[string]func(){
		"a second image kind": func() {
			stub.RegisterInput(stub.InputKind{Name: "image", Matcher: stub.NearestMatcher, Item: func(*contract.Tensor, *match.Reducer, *mappings.Mapping, json.RawMessage) ([]float64, error) {
				return nil, nil
			}})
		},
		"a kind of a matcher stubs do not have": func() { stub.RegisterInput(stub.InputKind{Name: "phrase", Matcher: "contains"}) },
		"a second classification task":          func() { task.Register(task.Adapter{Task: "classification"}) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s is registered", name)
				}
			}()
			register()
		}()
	}
}
