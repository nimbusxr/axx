//go:build integration

package modelscore

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// Phrases of one speaker are told apart when a platform speech stub takes
// Whisper's log-mel features: the microphone step matches each recording
// with the reference, and a phrase no mapping has gets the default.
func TestMicrophoneMatchesOneSpeakersPhrases(t *testing.T) {
	f := newMicrophoneFixture(t)
	dir := t.TempDir()
	utterances := modelstest.Speech(t, dir, 16000)
	c := &contract.Contract{
		Format: contract.Format{Type: "platform", Details: []byte(`{"api":"ios.speech"}`)},
		Task:   "speech-to-text",
		Inputs: []contract.Tensor{{
			Name: "features", Type: "float32", Shape: []contract.Dim{{Size: 1}, {Size: 80}, {Size: 3000}},
			Audio: &contract.Audio{SampleRate: 16000, Features: "log-mel"},
		}},
	}
	if err := c.Save(filepath.Join(dir, contract.FileName)); err != nil {
		t.Fatal(err)
	}
	s, err := stub.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := platform.Write(s)
	if err != nil {
		t.Fatal(err)
	}
	owner, _ := f.h.Suite.FileOwner("app")
	models, err := owner(f.h.SC, "courier", platform.Folder)
	if err != nil {
		t.Fatal(err)
	}
	if err := models.Write(f.h.SC.Context(), "speech.axxmodel", b); err != nil {
		t.Fatal(err)
	}
	for i, u := range utterances {
		wav := fmt.Sprintf("utterance-%d.wav", i)
		modelstest.WriteWAV(t, filepath.Join(f.h.Dir, wav), 16000, u.Samples)
		f.h.OK(`the courier app's microphone hears the "` + wav + `" file`)
		r, err := TakeRecording(f.h.SC, "courier")
		if err != nil || r == nil {
			t.Fatalf("%s: %v", u.Name, err)
		}
		if got := r.Matches["ios.speech"]; got != u.Mapping {
			t.Errorf("%s: matches mapping %d, want %d", u.Name, got, u.Mapping)
		}
	}
}
