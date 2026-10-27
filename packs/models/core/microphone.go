package modelscore

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/match"
	"github.com/nimbusxr/axx/packs/models/core/stub"
)

// MicrophoneFolder is transient recognition input, not a generated model. Its
// hidden folder is ignored when model artifacts are loaded at app startup.
const MicrophoneFolder = "axx-models/.microphone"

// Recording is decoded PCM, resampled by the same reference used to build
// audio mapping keys. Adapters consume recordings in filename order.
// Matches are the mapping each platform speech stub of the app answers the
// recording with (its index, the default's last), by the stub's API: the
// reference matches it, so an adapter answers it as axx does.
type Recording struct {
	Rate    int            `json:"rate"`
	Samples []float64      `json:"samples"`
	Matches map[string]int `json:"matches,omitempty"`
}

type microphoneState struct {
	mu         sync.Mutex
	next, read map[string]int
	// fed are the apps whose platform pack plays recordings into the app's
	// own audio capture.
	fed map[string]bool
}

var microphones = core.NewStateKey(Name+"/microphones", func(*core.Scenario) *microphoneState {
	return &microphoneState{next: map[string]int{}, read: map[string]int{}, fed: map[string]bool{}}
}, nil)

// FeedsMicrophone says the app's platform pack plays the app's recordings
// into its own audio capture (what it records itself), not only to its
// platform speech stubs: the microphone step then needs no speech stub.
func FeedsMicrophone(sc *core.Scenario, app string) {
	s := microphones.Of(sc)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fed[sc.Suite().Interpolate(app)] = true
}

// microphoneScenarios are the run's scenarios whose steps include the
// microphone step, by the plan.
func microphoneScenarios(s *core.Suite) map[string]bool {
	ids, _ := core.Cached(s, Name+"/microphone-scenarios", func() (map[string]bool, error) { return map[string]bool{}, nil })
	return ids
}

// UsesMicrophone says the scenario's steps include the microphone step: a
// platform pack prepares the app's audio capture before the app starts.
func UsesMicrophone(sc *core.Scenario) bool { return microphoneScenarios(sc.Suite())[sc.ID] }

// Prepare finds the scenarios that use the microphone step.
func (pack) Prepare(_ context.Context, s *core.Suite, plan *core.Plan) error {
	ids := microphoneScenarios(s)
	for _, sc := range plan.Scenarios {
		for _, st := range sc.Steps {
			if st.Definition == Name+".microphone" {
				ids[sc.ID] = true
			}
		}
	}
	return nil
}

func microphoneSteps() []core.StepDef {
	return []core.StepDef{{
		ID: Name + ".microphone", Keyword: "When", Since: "0.2.0",
		Expr:     "the {word} app's microphone hears the {string} file",
		Doc:      "The app's microphone hears a recording (a WAV file). Recordings wait, in order, until the app listens: the app's platform speech stub answers the mapping the recording matches (axx matches it as the stub's keys were made), and a web page's own microphone (getUserMedia) records it. The file's name selects no answer.",
		Examples: []string{`When the courier app's microphone hears the "recordings/door.wav" file`},
		Run:      func(sc *core.Scenario, a core.Args) error { return microphone(sc, a.String(0), a.String(1)) },
	}}
}

func microphone(sc *core.Scenario, app, file string) error {
	app = sc.Suite().Interpolate(app)
	owner, ok := sc.Suite().FileOwner("app")
	if !ok {
		return microphoneError("no app file owner is installed")
	}
	models, err := owner(sc, app, platform.Folder)
	if err != nil {
		return err
	}
	artifacts, err := platform.Load(sc.Context(), models)
	if err != nil {
		return err
	}
	rate := 0
	var speech []*platform.Artifact
	for _, a := range artifacts {
		if a.Contract.Task != "speech-to-text" {
			continue
		}
		in := a.Contract.MatchedInput()
		if in == nil || in.Audio == nil {
			continue
		}
		if rate != 0 && rate != in.Audio.SampleRate {
			return microphoneError("the app's speech stubs use different sample rates")
		}
		rate = in.Audio.SampleRate
		speech = append(speech, a)
	}
	s := microphones.Of(sc)
	s.mu.Lock()
	fed := s.fed[app]
	s.mu.Unlock()
	if rate == 0 && !fed {
		return microphoneError("the %s app has no platform speech stub in axx-models, and its platform pack does not play recordings into the app's own audio capture", app)
	}
	path, err := sc.ResolvePath(file)
	if err != nil {
		return err
	}
	var samples []float64
	if rate == 0 {
		// Nothing matches it: the app's capture hears it at the file's rate.
		samples, rate, err = match.ReadSoundRate(path)
	} else {
		samples, err = match.ReadSound(path, rate)
	}
	if err != nil {
		return microphoneError("%v", err)
	}
	matches := map[string]int{}
	for _, a := range speech {
		item, err := stub.SoundItem(a.Contract.MatchedInput(), samples)
		if err != nil {
			return microphoneError("%v", err)
		}
		if matches[a.API], err = a.Index(item); err != nil {
			return microphoneError("%v", err)
		}
	}
	data, err := json.Marshal(Recording{Rate: rate, Samples: samples, Matches: matches})
	if err != nil {
		return err
	}
	files, err := owner(sc, app, MicrophoneFolder)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	n := s.next[app] + 1
	if err := files.Write(sc.Context(), fmt.Sprintf("%08d.json", n), data); err != nil {
		return err
	}
	s.next[app] = n
	return nil
}

func microphoneError(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, "microphone input: "+f, args...).WithHint("copy a prebuilt platform speech stub to the app's axx-models folder before supplying a recording")
}

// TakeRecording claims the next queued recording for browser adapters. Native
// adapters use the same .taken acknowledgement in the app's own filesystem.
func TakeRecording(sc *core.Scenario, app string) (*Recording, error) {
	app = sc.Suite().Interpolate(app)
	owner, ok := sc.Suite().FileOwner("app")
	if !ok {
		return nil, microphoneError("no app file owner is installed")
	}
	files, err := owner(sc, app, MicrophoneFolder)
	if err != nil {
		return nil, err
	}
	s := microphones.Of(sc)
	s.mu.Lock()
	defer s.mu.Unlock()
	for n := s.read[app] + 1; n <= s.next[app]; n++ {
		name := fmt.Sprintf("%08d.json", n)
		if _, ok, err := files.Read(sc.Context(), name+".taken"); err != nil {
			return nil, err
		} else if ok {
			s.read[app] = n
			continue
		}
		b, ok, err := files.Read(sc.Context(), name)
		if err != nil {
			return nil, err
		}
		if !ok {
			s.read[app] = n
			continue
		}
		var recording Recording
		if err := json.Unmarshal(b, &recording); err != nil || recording.Rate <= 0 {
			return nil, microphoneError("invalid recording %s", name)
		}
		if err := files.Write(sc.Context(), name+".taken", []byte{}); err != nil {
			return nil, err
		}
		s.read[app] = n
		return &recording, nil
	}
	s.read[app] = s.next[app]
	return nil, nil
}
