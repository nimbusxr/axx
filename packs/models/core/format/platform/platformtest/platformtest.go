// Package platformtest builds platform API stubs for the surface packs' tests.
package platformtest

import (
	"image"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Artifact builds a platform API's stub from the contract
// `axx models contract <api>` writes for it (with labels, for a labeler),
// a mapping of the door's picture, the doorbell or the text "parcel" (as
// the API takes), and a default, and reads it back as its adapter does.
func Artifact(t *testing.T, api platform.API, labels []string) *platform.Artifact {
	t.Helper()
	c, err := api.Contract(labels)
	if err != nil {
		t.Fatalf("%s: %v", api.Name, err)
	}
	dir := modelstest.Dir(t, c)
	key := map[string]any{"image": "../inputs/door.png"}
	var answer, otherwise map[string]any
	switch api.Task {
	case "classification":
		out := c.Outputs[0].Labels
		answer, otherwise = map[string]any{"label": out[0], "score": 0.9}, map[string]any{"label": out[len(out)-1]}
	case "text-recognition":
		answer, otherwise = map[string]any{"text": "PX-4111 Leipzig"}, map[string]any{"text": ""}
	case "detection":
		box := map[string]any{"x": 0.125, "y": 0.5, "width": 0.33, "height": 0.33}
		answer = map[string]any{"objects": []any{map[string]any{"label": c.Outputs[1].Labels[0], "box": box, "score": 0.9}}}
		otherwise = map[string]any{"objects": []any{}}
	case "segmentation":
		mask := image.NewGray(image.Rect(0, 0, 32, 32))
		for y := 8; y < 24; y++ {
			for x := 8; x < 24; x++ {
				mask.SetGray(x, y, color.Gray{Y: 255})
			}
		}
		modelstest.WritePNG(t, filepath.Join(dir, "inputs", "person.png"), mask)
		modelstest.WritePNG(t, filepath.Join(dir, "inputs", "nobody.png"), image.NewGray(image.Rect(0, 0, 32, 32)))
		answer, otherwise = map[string]any{"mask": "../inputs/person.png"}, map[string]any{"mask": "../inputs/nobody.png"}
	case "speech-to-text":
		in := c.MatchedInput()
		modelstest.WriteWAV(t, filepath.Join(dir, "inputs", "bell.wav"), in.Audio.SampleRate, modelstest.Doorbell.Samples(in.Audio.SampleRate, 1, 0, 1, 0))
		key = map[string]any{"audio": "../inputs/bell.wav"}
		answer, otherwise = map[string]any{"text": "Ring twice, please."}, map[string]any{"text": "Please try again."}
	case "text-generation":
		key = map[string]any{"text": "parcel"}
		answer, otherwise = map[string]any{"text": "Ring twice, please."}, map[string]any{"text": "Please try again."}
	default:
		t.Fatalf("%s: no sample mapping for task %s", api.Name, api.Task)
	}
	if _, ok := key["image"]; ok {
		modelstest.Door.Write(t, filepath.Join(dir, "inputs", "door.png"))
	}
	modelstest.WriteMappings(t, dir, []map[string]any{
		{"name": "The key", "input": key, "output": answer},
		{"name": "Anything else", "default": true, "output": otherwise},
	})
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatalf("%s: %v", api.Name, err)
	}
	b, err := platform.Write(s)
	if err != nil {
		t.Fatalf("%s: %v", api.Name, err)
	}
	a, err := platform.Read(b)
	if err != nil {
		t.Fatalf("%s: %v", api.Name, err)
	}
	return a
}
