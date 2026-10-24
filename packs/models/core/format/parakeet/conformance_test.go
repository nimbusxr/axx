//go:build integration

package parakeet

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/internal/parakeetrun"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// constantModel is the smallest end-to-end model used by the loader test.
func constantModel() []byte { return buildModel(nil, nil, "hello", 0.9, match.ParakeetMels) }

// The real Parakeet CLI loads a minimal CTC model, including its encoder and
// CTC head, and transcribes with it.
func TestConstantCTCModelLoadsAndTranscribes(t *testing.T) {
	rt, err := parakeetrun.Install(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	model := filepath.Join(dir, "stub.gguf")
	if err := os.WriteFile(model, constantModel(), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := rt.Run(t.Context(), dir, "info", "/work/stub.gguf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(info, "ctc") {
		t.Errorf("model info does not identify CTC: %s", info)
	}
	t.Logf("model info:\n%s", info)
	wav := filepath.Join(dir, "audio.wav")
	modelstest.WriteWAV(t, wav, 16000, make([]float64, 1600))
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	out, err := rt.Run(ctx, dir, "transcribe", "--model", "/work/stub.gguf", "--input", "/work/audio.wav", "--decoder", "ctc", "--threads", "1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("transcription %q does not contain the model's answer", out)
	}
}

func TestStubTranscribesAudioMappings(t *testing.T) {
	// At 16 kHz, Parakeet's rate, and at a telephone's 8 kHz, which
	// parakeet.cpp resamples as the stub's keys are.
	for _, rate := range []int{16000, 8000} {
		t.Run(fmt.Sprintf("%d Hz", rate), func(t *testing.T) { transcribes(t, rate) })
	}
}

func transcribes(t *testing.T, rate int) {
	s, sounds := speechStub(t, rate)
	model, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := parakeetrun.Install(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "stub.gguf"), model, 0o644); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		name string
		data []float64
		want string
	}{
		{"doorbell", sounds[0], "Leave the parcel with the neighbour."},
		{"knock", sounds[1], "The box is crushed."},
		{"hum", sounds[2], "No note."},
		// As a phone records them: later, quieter, with noise.
		{"doorbell, recorded", modelstest.Doorbell.Samples(rate, 1, rate/10, 0.5, 0.01), "Leave the parcel with the neighbour."},
		{"knock, recorded", modelstest.Knock.Samples(rate, 1, rate/10, 0.5, 0.01), "The box is crushed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := strings.NewReplacer(" ", "-", ",", "").Replace(tc.name)
			wav := filepath.Join(dir, name+".wav")
			modelstest.WriteWAV(t, wav, rate, tc.data)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			out, err := rt.Run(ctx, dir, "transcribe", "--model", "/work/stub.gguf", "--input", "/work/"+name+".wav", "--threads", "1")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("transcription %q does not contain %q", out, tc.want)
			}
			reduced, err := s.Nearest.Reducer.Reduce(match.ParakeetLogMel(match.ParakeetResample(tc.data, rate), 16000, match.ParakeetMels))
			if err != nil {
				t.Fatal(err)
			}
			idx := s.Nearest.Table.Nearest(reduced)
			t.Logf("similarities %.4f %.4f; selected %d", match.Similarity(reduced, s.Nearest.Table.Keys[0]), match.Similarity(reduced, s.Nearest.Table.Keys[1]), idx)
			if want := i % 3; idx != want {
				t.Errorf("the reference selected mapping %d, want %d", idx, want)
			}
		})
	}
}

func speechStub(t *testing.T, rate int) (*stub.Stub, [][]float64) {
	t.Helper()
	c := &contract.Contract{
		Format: contract.Format{Type: "parakeet"},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			// NVIDIA's parakeet-ctc-0.6b's: a stub's frontend is its own.
			Name: "mel", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 80}, {Size: 3000}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "parakeet"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: 4}}}},
	}
	dir := modelstest.Dir(t, c,
		map[string]any{"name": "Neighbour", "input": map[string]any{"audio": "../sounds/doorbell.wav"}, "output": map[string]any{"text": "Leave the parcel with the neighbour."}},
		map[string]any{"name": "Crushed", "input": map[string]any{"audio": "../sounds/knock.wav"}, "output": map[string]any{"text": "The box is crushed."}},
		map[string]any{"name": "Other", "default": true, "output": map[string]any{"text": "No note."}},
	)
	sounds := [][]float64{
		modelstest.Doorbell.Samples(rate, 1, 0, 1, 0),
		modelstest.Knock.Samples(rate, 1, 0, 1, 0),
		modelstest.Hum.Samples(rate, 1, 0, 1, 0),
	}
	sounds[2] = make([]float64, rate)
	for i, name := range []string{"doorbell", "knock", "hum"} {
		modelstest.WriteWAV(t, filepath.Join(dir, "sounds", name+".wav"), rate, sounds[i])
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, sounds
}
