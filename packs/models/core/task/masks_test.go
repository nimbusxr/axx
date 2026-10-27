package task

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// A text detector's answer is a picture a label: each pixel's confidence
// of the label, 0 where a label has none.
func TestMasksGiveEachLabelsConfidences(t *testing.T) {
	c := &contract.Contract{Task: "segmentation", Outputs: []contract.Tensor{{Name: "maps", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 2}, {Size: 2}, {Size: 2}}, Labels: []string{"region", "affinity"}}}}
	img := image.NewGray(image.Rect(0, 0, 2, 2))
	img.SetGray(1, 0, color.Gray{Y: 255})
	img.SetGray(0, 1, color.Gray{Y: 51})
	path := filepath.Join(t.TempDir(), "region.png")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	masks, _ := json.Marshal(map[string]string{"region": path})
	out := Values{}
	if err := segment(c, map[string]json.RawMessage{"masks": masks}, out); err != nil {
		t.Fatal(err)
	}
	want := []float64{0, 0, 1, 0, 0.2, 0, 0, 0} // [y][x][region, affinity]
	got := out["maps"]
	for i := range want {
		if d := got[i] - want[i]; d > 1e-6 || d < -1e-6 {
			t.Fatalf("maps %v, want %v", got, want)
		}
	}
	bad, _ := json.Marshal(map[string]string{"link": path})
	if err := segment(c, map[string]json.RawMessage{"masks": bad}, Values{}); err == nil {
		t.Error("a mask of a label the detector does not have is no mistake")
	}
}
