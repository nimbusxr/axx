package parakeet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/gguf"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// A real model's contract (NVIDIA's Parakeet TDT-CTC 110M's: 80 mels) gives a
// stub of its own frontend, which parakeet.cpp reads from its file.
func TestAStubOfARealModelsContractHasItsOwnFrontend(t *testing.T) {
	c := &contract.Contract{
		Format: contract.Format{Type: "parakeet", Version: 3},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "mel", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 80}, {Size: 3000}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "parakeet"},
		}},
		Outputs: []contract.Tensor{{Name: "logits", Type: "float32", Shape: []contract.Dim{{Name: "batch"}, {Name: "sequence"}, {Size: 1025}}}},
	}
	dir := modelstest.Dir(t, c,
		map[string]any{"name": "Neighbour", "input": map[string]any{"audio": "../sounds/doorbell.wav"}, "output": map[string]any{"text": "Leave the parcel with the neighbour."}},
		map[string]any{"name": "Other", "default": true, "output": map[string]any{"text": "No note."}},
	)
	modelstest.WriteWAV(t, filepath.Join(dir, "sounds", "doorbell.wav"), 8000, modelstest.Doorbell.Samples(8000, 1, 0, 1, 0))
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nearest.Table.Keys[0]) != match.ParakeetMels {
		t.Fatalf("a key of %d values, want %d", len(s.Nearest.Table.Keys[0]), match.ParakeetMels)
	}
	b, err := Write(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "stub.gguf")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := gguf.Read(path, true)
	if err != nil {
		t.Fatal(err)
	}
	if mels, _ := f.Uint("parakeet.preprocessor.n_mels"); mels != match.ParakeetMels || !isParakeet(path) {
		t.Errorf("the stub's frontend has %d mels, or is no Parakeet model", mels)
	}
}
