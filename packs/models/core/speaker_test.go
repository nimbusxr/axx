package modelscore

import (
	"path/filepath"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
)

// The speaker step passes once an utterance of the app's has the recording's sound, at the
// voice's rate (the recording resampled to it, as the voice stub's build resamples it); other
// utterances, and none, fail it, saying what the app spoke.
func TestSpeakerHearsTheRecording(t *testing.T) {
	h := cloudtest.New(t, Pack())
	h.Start(&core.Plan{})
	root := t.TempDir()
	h.Suite.SetFileOwner("app", func(_ *core.Scenario, name, path string) (core.Files, error) {
		return core.LocalFiles(filepath.Join(root, name, path)), nil
	})
	rules := modelstest.Doorbell.Samples(22050, 0.5, 0, 1, 0)
	modelstest.WriteWAV(t, filepath.Join(h.Dir, "rules.wav"), 22050, rules)
	spoken := filepath.Join(root, "depot", SpeakerFolder)

	_ = h.Fails(`within 1s the depot app's speaker plays the "rules.wav" file`, "played nothing")
	// Another utterance, at the voice's rate.
	modelstest.WriteWAV(t, filepath.Join(spoken, "0001.wav"), 16000, modelstest.Knock.Samples(16000, 0.5, 0, 1, 0))
	_ = h.Fails(`within 1s the depot app's speaker plays the "rules.wav" file`, "played 1 utterance, not rules.wav: it differs from it at")
	// The rules, spoken by a voice of 16 kHz: the recording at its rate, in 16-bit PCM.
	modelstest.WriteWAV(t, filepath.Join(spoken, "0002.wav"), 16000, match.Resample(rules, 22050, 16000))
	h.OK(`the depot app's speaker plays the "rules.wav" file`)
}
