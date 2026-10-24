//go:build integration

package piper

import (
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/internal/piperrun"
)

// The real PiperVoice loads the stub and its sidecar and synthesizes each
// mapping's text, and a text that has one, in any case (Piper reads it
// case-folded). Constant waveforms make the answer unambiguous even after
// Piper's int16 peak normalization.
func TestPiperRuntimeSynthesizesMappings(t *testing.T) {
	s := voiceStub(t)
	m, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := piperrun.Install(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	answers, err := rt.Run(t.Context(), []piperrun.Job{
		{Model: m.File, Config: m.Beside[".onnx.json"], Text: "ring twice"},
		{Model: m.File, Config: m.Beside[".onnx.json"], Text: "fragile 🙂"},
		{Model: m.File, Config: m.Beside[".onnx.json"], Text: "something else"},
		{Model: m.File, Config: m.Beside[".onnx.json"], Text: "Please RING TWICE at the door"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int16{32767, -32767, 0, 32767} {
		if len(answers[i]) != 64 {
			t.Errorf("answer %d has %d samples, want 64", i, len(answers[i]))
			continue
		}
		for j, got := range answers[i] {
			if got != want {
				t.Errorf("answer %d sample %d is %d, want %d", i, j, got, want)
				break
			}
		}
	}
}
