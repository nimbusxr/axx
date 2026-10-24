package modelscore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/cloudstep/cloudtest"
	"github.com/nimbusxr/axx/packs/models/core/contract"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/mappings"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/modelstest"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

type microphoneFixture struct {
	t     *testing.T
	h     *cloudtest.Harness
	wav   string
	roots map[*core.Scenario]string
}

func newMicrophoneFixture(t *testing.T) *microphoneFixture {
	t.Helper()
	f := &microphoneFixture{t: t, roots: map[*core.Scenario]string{}}
	f.h = cloudtest.New(t, Pack())
	f.h.Start(&core.Plan{})
	f.h.Suite.SetFileOwner("app", func(sc *core.Scenario, name, path string) (core.Files, error) {
		return core.LocalFiles(filepath.Join(f.root(sc), name, path)), nil
	})
	f.setup()
	f.wav = filepath.Join(f.h.Dir, "recording.wav")
	modelstest.WriteWAV(t, f.wav, 8000, modelstest.Doorbell.Samples(8000, 0.5, 0, 1, 0))
	return f
}

func (f *microphoneFixture) root(sc *core.Scenario) string {
	if f.roots[sc] == "" {
		f.roots[sc] = f.t.TempDir()
	}
	return f.roots[sc]
}

func (f *microphoneFixture) setup() {
	owner, _ := f.h.Suite.FileOwner("app")
	for _, app := range []string{"courier", "other"} {
		files, err := owner(f.h.SC, app, "axx-models")
		if err != nil {
			f.t.Fatal(err)
		}
		c := modelstest.SoundClassifier()
		c.Task = "speech-to-text"
		c.Outputs = nil
		c.Format = contract.Format{Type: "platform", Details: json.RawMessage(`{"api":"android.speech"}`)}
		s, err := stub.Resolve(c, []mappings.Mapping{{Name: "default", Default: true, Output: json.RawMessage(`{"text":"answer"}`)}})
		if err != nil {
			f.t.Fatal(err)
		}
		b, err := platform.Write(s)
		if err != nil {
			f.t.Fatal(err)
		}
		if err := files.Write(f.h.SC.Context(), "speech.axxmodel", b); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestMicrophoneQueuesAudioAndSeparatesAppsAndScenarios(t *testing.T) {
	f := newMicrophoneFixture(t)
	want, err := match.ReadSound(f.wav, 16000)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		f.h.OK(`the courier app's microphone hears the "recording.wav" file`)
	}
	if r, err := TakeRecording(f.h.SC, "other"); r != nil || err != nil {
		t.Fatalf("another app got input: %v %v", r, err)
	}
	for range 2 {
		r, err := TakeRecording(f.h.SC, "courier")
		if err != nil || r == nil || r.Rate != 16000 || !reflect.DeepEqual(r.Samples, want) {
			t.Fatalf("recording disagrees with decoded audio: %v", err)
		}
		if !reflect.DeepEqual(r.Matches, map[string]int{"android.speech": 0}) {
			t.Fatalf("recording's matches %v, want the speech stub's default", r.Matches)
		}
	}
	if r, err := TakeRecording(f.h.SC, "courier"); r != nil || err != nil {
		t.Fatalf("consumed recording was replayed: %v %v", r, err)
	}
	f.h.NewScenario()
	f.setup()
	if r, err := TakeRecording(f.h.SC, "courier"); r != nil || err != nil {
		t.Fatalf("new scenario inherited input: %v %v", r, err)
	}
	f.h.OK(`the courier app's microphone hears the "recording.wav" file`)
	if _, err := os.Stat(filepath.Join(f.root(f.h.SC), "courier", MicrophoneFolder, "00000001.json")); err != nil {
		t.Fatalf("new scenario did not reset the sequence: %v", err)
	}
}

func TestMicrophoneInterpolatedAndLiteralNamesShareSequence(t *testing.T) {
	f := newMicrophoneFixture(t)
	t.Setenv("AXX_TEST_APP_NAME", "courier")
	if err := microphone(f.h.SC, "${env:AXX_TEST_APP_NAME}", f.wav); err != nil {
		t.Fatal(err)
	}
	f.h.OK(`the courier app's microphone hears the "recording.wav" file`)
	for i := 1; i <= 2; i++ {
		if _, err := os.Stat(filepath.Join(f.root(f.h.SC), "courier", MicrophoneFolder, fmt.Sprintf("%08d.json", i))); err != nil {
			t.Fatalf("recording %d was overwritten: %v", i, err)
		}
		r, err := TakeRecording(f.h.SC, "courier")
		if err != nil || r == nil {
			t.Fatalf("recording %d was not queued: %v", i, err)
		}
	}
}

type cappedFiles struct{ core.Files }

// List simulates a folder whose first 10,000 entries are old recordings and
// acknowledgements. A consumer that relies on that listing never sees a new
// recording after them.
func (cappedFiles) List(context.Context, int) ([]string, error) {
	names := make([]string, 10000)
	for i := range 5000 {
		names[2*i] = fmt.Sprintf("%08d.json", i+1)
		names[2*i+1] = names[2*i] + ".taken"
	}
	return names, nil
}

func TestMicrophoneQueueBeyondListingCap(t *testing.T) {
	f := newMicrophoneFixture(t)
	f.h.Suite.SetFileOwner("app", func(sc *core.Scenario, name, path string) (core.Files, error) {
		files := core.LocalFiles(filepath.Join(f.root(sc), name, path))
		if path == MicrophoneFolder {
			return cappedFiles{files}, nil
		}
		return files, nil
	})
	state := microphones.Of(f.h.SC)
	state.next["courier"] = 5000
	state.read["courier"] = 5000
	if err := microphone(f.h.SC, "courier", f.wav); err != nil {
		t.Fatal(err)
	}
	r, err := TakeRecording(f.h.SC, "courier")
	if err != nil || r == nil {
		t.Fatalf("the recording after 10,000 old entries was not delivered: %v", err)
	}
}
