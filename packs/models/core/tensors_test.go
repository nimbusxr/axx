package modelscore

import (
	"slices"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
)

// --input and --output give tensors their types and shapes, by name; an
// input of a picture's shape gets its layout; a mistake says what is wrong.
func TestFlagsGiveTensorsTheirTypesAndShapes(t *testing.T) {
	c := &contract.Contract{Inputs: []contract.Tensor{{Name: "images"}}, Outputs: []contract.Tensor{{Name: "output0"}, {Name: "output1"}}}
	if got := untyped(c); len(got) != 3 {
		t.Fatalf("untyped: %q", got)
	}
	err := giveTensors(c, []string{"images=float32[?,3,224,224]"}, []string{"output0=float32[?,1000]", "output1=int64[?]"})
	if err != nil {
		t.Fatal(err)
	}
	if in := c.Inputs[0]; in.Type != "float32" || !slices.Equal(in.Shape, []contract.Dim{{Name: "?"}, {Size: 3}, {Size: 224}, {Size: 224}}) || in.Image == nil || in.Image.Layout != "NCHW" {
		t.Errorf("images: %+v", in)
	}
	if out := c.Outputs[1]; out.Type != "int64" || len(out.Shape) != 1 || out.Shape[0].Name != "?" {
		t.Errorf("output1: %+v", out)
	}
	if got := untyped(c); len(got) != 0 {
		t.Errorf("untyped after: %q", got)
	}
	for _, bad := range []string{"images", "images=float32", "images=float31[1]", "images=float32[0,3]", "pixels=float32[1]"} {
		if err := giveTensors(c, []string{bad}, nil); err == nil {
			t.Errorf("--input %s is not a mistake", bad)
		}
	}
}
