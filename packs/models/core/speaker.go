package modelscore

import (
	"fmt"
	"math"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/cloudstep"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/packs/models/core/format/platform"
	"github.com/nimbusxr/axx/packs/models/core/match"
)

// SpeakerFolder is where a platform pack puts what the app speaks, a WAV file an utterance,
// named in the order the app spoke them: hidden in the app's axx-models folder, as the
// microphone's recordings are.
const SpeakerFolder = "axx-models/.speaker"

// spoken is how far an utterance's sample may be from the recording's: the 16-bit PCM a voice
// writes rounds them.
const spoken = 2.0 / 32768

func speakerSteps() []core.StepDef {
	return []core.StepDef{{
		ID: Name + ".speaker", Keyword: "Then", Since: "0.2.0",
		Expr: "[[within {duration} ]]the {word} app's speaker plays the {string} file",
		Doc: "Check that the app has spoken a recording (a WAV file): an utterance of its speech has the recording's sound. The app speaks with a voice stub, which speaks a mapping's recording for a text that has the mapping's key: the file is the recording the app's words are spoken as. " +
			"The app's platform pack hears its speech (models-linux: speech-dispatcher's, in the desktops axx runs). The check waits for it: 10 seconds, or `within {duration}`.",
		Examples: []string{`Then within 10s the depot app's speaker plays the "recordings/handover-rules.wav" file`},
		Run: func(sc *core.Scenario, a core.Args) error {
			return speaker(sc, cloudstep.Wait(a, 0), a.String(1), a.String(2))
		},
	}}
}

func speaker(sc *core.Scenario, wait time.Duration, app, file string) error {
	app = sc.Suite().Interpolate(app)
	owner, ok := sc.Suite().FileOwner("app")
	if !ok {
		return speakerError("no app file owner is installed")
	}
	utterances, err := owner(sc, app, SpeakerFolder)
	if err != nil {
		return err
	}
	resolved, err := sc.ResolvePath(file)
	if err != nil {
		return err
	}
	recording, rate, err := match.ReadSoundRate(resolved)
	if err != nil {
		return speakerError("%v", err)
	}
	ctx := sc.Context()
	return cloudstep.Poll(sc, wait, func() (bool, string, error) {
		names, err := utterances.List(ctx, 1000)
		if err != nil {
			return false, "", err
		}
		names = slices.DeleteFunc(names, func(n string) bool { return !strings.EqualFold(path.Ext(n), ".wav") })
		if len(names) == 0 {
			return false, fmt.Sprintf("the %s app's speaker played nothing: its platform pack hears the speech of its platform's speech service (models-linux: speech-dispatcher's), spoken with a voice stub in its axx-models folder", app), nil
		}
		slices.Sort(names)
		closest := ""
		for _, n := range names {
			b, ok, err := utterances.Read(ctx, n)
			if err != nil || !ok {
				return false, "", err
			}
			samples, at, err := match.DecodeSound(b)
			if err != nil {
				return false, "", speakerError("the app's utterance %s: %v", n, err)
			}
			if why := same(samples, match.Resample(recording, rate, at), at); why == "" {
				return true, "", nil
			} else if closest == "" {
				closest = why
			}
		}
		if len(names) == 1 {
			return false, fmt.Sprintf("the %s app's speaker played 1 utterance, not %s: it %s", app, file, closest), nil
		}
		return false, fmt.Sprintf("the %s app's speaker played %d utterances, none of them %s: the first %s", app, len(names), file, closest), nil
	})
}

// same is "" when an utterance has the recording's sound, or what is different.
func same(utterance, recording []float64, rate int) string {
	for i := range max(len(utterance), len(recording)) {
		var u, r float64
		if i < len(utterance) {
			u = utterance[i]
		}
		if i < len(recording) {
			r = recording[i]
		}
		if math.Abs(u-r) > spoken {
			return fmt.Sprintf("differs from it at %.2fs (%d samples, against %d)", float64(i)/float64(rate), len(utterance), len(recording))
		}
	}
	return ""
}

func speakerError(f string, args ...any) error {
	return axxerr.New(platform.CodeArtifact, exitcode.Usage, "speaker: "+f, args...).WithHint("copy the app's voice stub to its axx-models folder before the app speaks, on a platform whose models pack hears its speech")
}
