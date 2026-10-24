package openaiwhisper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/format"
	"github.com/nimbusxr/axx/packs/models/core/internal/whisper"
)

// A checkpoint is torch.save's of its dims and weights: its dims are read
// as they are saved, and it is told from a TorchScript module by its
// content (both are .pt files).
func TestACheckpointsDimsAreReadAsTheyAreSaved(t *testing.T) {
	d := whisper.Dims{Mels: 80, Vocab: 51864, D: 384, Heads: 6, EncoderLayers: 4, DecoderLayers: 4, EncoderFFN: 1536, DecoderFFN: 1536, AudioPositions: 1500, TextPositions: 448}
	w := whisper.Weights{
		"encoder.conv1.weight": {Shape: []int{384, 80, 3}, Data: make([]float32, 384*80*3)},
		"encoder.conv1.bias":   {Shape: []int{384}, Data: make([]float32, 384)},
	}
	b, err := save(d, w)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "tiny.en.pt")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if !isCheckpoint(path) {
		t.Fatal("a checkpoint is not told by its content")
	}
	dm, err := dims(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]int{"n_mels": 80, "n_vocab": 51864, "n_audio_state": 384, "n_text_head": 6, "n_audio_ctx": 1500, "n_text_ctx": 448} {
		if dm[name] != want {
			t.Errorf("%s is %d, want %d", name, dm[name], want)
		}
	}
	if f, ok := format.ByFile(path); !ok || f.Type != "openai-whisper" {
		t.Errorf("a checkpoint's format is %q", f.Type)
	}
	// Its contract wants the package's vocabulary beside it.
	if _, err := ReadContract(path); err == nil {
		t.Error("a checkpoint of no vocabulary beside it has a contract")
	}
}
