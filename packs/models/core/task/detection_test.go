package task

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

func detector(outputs ...contract.Tensor) *contract.Contract {
	return &contract.Contract{
		Format: contract.Format{Type: "tflite"}, Task: "detection",
		Inputs:  []contract.Tensor{{Name: "pixels", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 100}, {Size: 200}, {Size: 3}}, Image: &contract.Image{Layout: "NHWC"}}},
		Outputs: outputs,
	}
}

var parcelLabels = []string{"carton", "envelope", "packet"}

func ssd() *contract.Contract {
	return detector(
		contract.Tensor{Name: "boxes", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}, {Size: 4}}, Detection: "boxes"},
		contract.Tensor{Name: "classes", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}}, Labels: parcelLabels, Detection: "classes"},
		contract.Tensor{Name: "scores", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}}, Detection: "scores"},
		contract.Tensor{Name: "count", Type: "float32", Shape: []contract.Dim{{Size: 1}}, Detection: "count"},
	)
}

const twoParcels = `{"objects": [
	{"label": "packet", "box": {"x": 0.5, "y": 0.25, "width": 0.25, "height": 0.5}, "score": 0.8},
	{"label": "carton", "box": {"x": 0, "y": 0, "width": 0.5, "height": 0.5}}
]}`

// A detector that suppresses boxes itself gives the objects first, in
// TensorFlow's order of a box's values unless its contract says.
func TestADetectorsAnswerIsItsObjects(t *testing.T) {
	c := ssd()
	if err := Check(c); err != nil {
		t.Fatal(err)
	}
	out, err := Answer(c, json.RawMessage(twoParcels))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]float64{
		"boxes":   {0.25, 0.5, 0.75, 0.75, 0, 0, 0.5, 0.5, 0, 0, 0, 0},
		"classes": {2, 0, 0},
		"scores":  {0.8, 1, 0},
		"count":   {2},
	} {
		if !near(out[name], want) {
			t.Errorf("%s: %v, want %v", name, out[name], want)
		}
	}
	c.Outputs[0].Box = &contract.Box{Order: "x,y,width,height", Pixels: true}
	out, err = Answer(c, json.RawMessage(twoParcels))
	if err != nil {
		t.Fatal(err)
	}
	if want := []float64{100, 25, 50, 50}; !near(out["boxes"][:4], want) {
		t.Errorf("a box in pixels from its corner: %v, want %v", out["boxes"][:4], want)
	}
}

// A detector whose app suppresses boxes gives the objects in its first
// anchors (YOLO's center and size, in pixels, unless its contract says).
func TestAnAnchorDetectorsAnswer(t *testing.T) {
	c := detector(contract.Tensor{Name: "anchors", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 7}, {Size: 5}}, Labels: parcelLabels, Detection: "anchors"})
	out, err := Answer(c, json.RawMessage(twoParcels))
	if err != nil {
		t.Fatal(err)
	}
	// By value: x of every anchor, then y, width, height, then each label's
	// scores.
	want := []float64{
		125, 50, 0, 0, 0,
		50, 25, 0, 0, 0,
		50, 100, 0, 0, 0,
		50, 50, 0, 0, 0,
		0, 1, 0, 0, 0,
		0, 0, 0, 0, 0,
		0.8, 0, 0, 0, 0,
	}
	if !near(out["anchors"], want) {
		t.Errorf("anchors %v, want %v", out["anchors"], want)
	}
}

func TestADetectorsAnswerIsChecked(t *testing.T) {
	c := ssd()
	for answer, want := range map[string]string{
		`{"objects": [{"label": "tube", "box": {"x": 0, "y": 0, "width": 1, "height": 1}}]}`:                                                                                                      `"tube" is not one of the detector's 3 labels`,
		`{"objects": [{"label": "carton"}]}`:                                                                                                                                                      "has no box",
		`{"objects": [{"label": "carton", "box": {"x": 0.5, "y": 0, "width": 0.6, "height": 1}}]}`:                                                                                                "not inside the picture",
		`{"objects": [{"label": "carton", "box": {"x": 0, "y": 0, "width": 1, "height": 1}, "score": 2}]}`:                                                                                        "not from 0 to 1",
		`{"objects": [{"label": "carton", "box": {"x": 0, "y": 0, "width": 1, "height": 1}, "kind": 1}]}`:                                                                                         "kind",
		`{"objects": [` + strings.Repeat(`{"label": "carton", "box": {"x": 0, "y": 0, "width": 1, "height": 1}},`, 3) + `{"label": "carton", "box": {"x": 0, "y": 0, "width": 1, "height": 1}}]}`: "4 objects: the detector gives 3 at most",
	} {
		if _, err := Answer(c, json.RawMessage(answer)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", answer, err, want)
		}
	}
	c.Outputs[1].Labels = nil
	if err := Check(c); err == nil || !strings.Contains(err.Error(), "labels") {
		t.Errorf("a detector without labels: %v", err)
	}
}

func TestADetectorsOutputsAreNamed(t *testing.T) {
	c := ssd()
	for i := range c.Outputs {
		c.Outputs[i].Detection, c.Outputs[i].Labels = "", nil
	}
	if err := DetectionRoles(c, parcelLabels); err != nil {
		t.Fatal(err)
	}
	if c.Outputs[0].Detection != "boxes" || c.Outputs[3].Detection != "count" || len(c.Outputs[1].Labels) != 3 {
		t.Errorf("outputs %+v", c.Outputs)
	}
	c.Outputs = c.Outputs[:2]
	if err := DetectionRoles(c, parcelLabels); err == nil {
		t.Error("two outputs are named")
	}
}
