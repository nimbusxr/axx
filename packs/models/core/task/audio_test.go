package task_test

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/task"
)

func TestTextToSpeechReadsAndResamplesAudio(t *testing.T) {
	c := &contract.Contract{
		Format: contract.Format{Type: "piper", Details: json.RawMessage(`{"sampleRate":16000}`)},
		Task:   "text-to-speech",
		Outputs: []contract.Tensor{{
			Name: "output", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 3}},
		}},
	}
	if err := task.Check(c); err != nil {
		t.Fatal(err)
	}
	wav := filepath.Join(t.TempDir(), "answer.wav")
	modelstest.WriteWAV(t, wav, 8000, []float64{0, 1, 0, -1})
	out, err := task.Answer(c, json.RawMessage(`{"audio":"`+wav+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(out["output"]) != 3 {
		t.Fatalf("output has %d samples, want 3", len(out["output"]))
	}
	if out["output"][0] == 0 && out["output"][1] == 0 && out["output"][2] == 0 {
		t.Fatal("the answer is silent after resampling")
	}
	if keys := task.FileKeys(c); len(keys) != 1 || keys[0] != "audio" {
		t.Errorf("file keys %v", keys)
	}
}

func TestTextToSpeechRequiresFixedWaveform(t *testing.T) {
	c := &contract.Contract{
		Format: contract.Format{Type: "piper", Details: json.RawMessage(`{"sampleRate":16000}`)},
		Task:   "text-to-speech",
		Outputs: []contract.Tensor{{
			Name: "output", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Name: "samples"}},
		}},
	}
	if err := task.Check(c); err == nil {
		t.Fatal("accepted an open waveform")
	}
}
