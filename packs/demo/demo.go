// Package demo is the demo pack: it makes a demo of a scenario. A demo is a
// feature file named *.demo.feature, whose scenario tells a story from its
// start to its end, with what to say to the people who watch in comments
// that start with "demo:" (script.go). `axx demo` runs it against the real
// system and keeps what the run did as a take (take.go), cuts the take into
// a film's timeline (cut.go), and films the stage, a page that draws any
// moment of it (film.go, stage/). What a demo shows ran, and passed.
//
// It has no steps: a demo's steps are the steps of the project's other
// packs. `axx run` runs a demo's file as any feature, and takes no notice of
// its cues.
package demo

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/internal/recording"
)

// Name is the pack's name.
const Name = "demo"

// Suffix ends the name of a demo's feature file.
const Suffix = ".demo.feature"

// Pack returns the demo pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: Name, Namespace: Name, Doc: packDoc, Experimental: true,
		Requires:     []string{"web-core"},
		ConfigSchema: []byte(configSchema), Steps: []core.StepDef{},
		Commands:      []core.Command{demoCommand()},
		FeatureChecks: []core.FeatureCheck{cuesCheck()},
		Hooks: []core.Hook{
			{ID: "demo.take", Phase: core.AfterScenario, Order: -1000, Run: keepTake},
			{ID: "demo.picture", Phase: core.AfterStep, Order: -1000, Run: pictureOf},
			{ID: "demo.gate", Phase: core.BeforeStep, Order: -1000, Run: gate},
			{ID: "demo.failed", Phase: core.StepFailed, Run: stepFailed},
		},
	}
}

const packDoc = `Make a demo of a scenario: a film of it, with subtitles, that shows only what ran and passed. A demo is a feature file named ` + "`*.demo.feature`" + `, with one scenario that tells a story from its start to its end, in the steps of the project's other packs. A comment that starts with ` + "`demo:`" + ` says what the people who watch read, and what it has in square brackets directs the film.

` + "```gherkin" + `
# demo: [card 3s] Holding a parcel at the depot
Feature: Hold a parcel

  Scenario: A shop holds a parcel for a recipient who is away
    # demo: [cut]
    Given the parcels service with the following properties:
      | property | value                      |
      | url      | http://${sys:local.host}:8080 |
    # demo: [action] Ms Okafor is away until Friday.
    #       Her shop asks us to hold her parcel.
    When ...
` + "```" + `

- **The film.** ` + "`axx demo features/hold.demo.feature`" + ` runs the scenario and writes the film beside what the run did (its take) in ` + "`.axx/demo`" + `. ` + "`--recut`" + ` makes the film again from the take, after a change to the subtitles or the directions, without another run.
- **What it shows.** A step on a screen shows the screen. Any other step shows a diagram of the system, drawn in characters: what the step sent travels along a line to its box, and what came back shows under it.
- **Your system's diagrams.** ` + "`diagrams`" + ` names diagrams of the system by name, each a file of a Mermaid flowchart or sequence diagram whose boxes and participants are named as the scenarios name what they talk to. A demo is drawn on the first, ` + "`[diagram name]`" + ` goes to another, and ` + "`[diagram one + other]`" + ` shows two side by side. ` + "`[as shop]`" + ` says which box the scenario is, an arrow, ` + "`[parcels --> addresses]`" + `, lights a line the scenario cannot see, and ` + "`[mark billing ✓]`" + ` writes in a box it does not talk to. Without a diagram, the picture is of what the scenario talked to.
- **A terminal.** ` + "`[show terminal]`" + ` shows the steps as someone would type them: a request is its curl command, typed out, with what came back printed under it. ` + "`[show terminal + diagram]`" + ` shows both.
- **Your own pictures.** A surface of your own is a scene, a few lines of JavaScript that draw the state of a moment: name it in ` + "`surfaces`" + `, its script in ` + "`scenes`" + `, and show it with ` + "`[show name]`" + `. A pack shares scenes, styles and directions as a module. The guide "Build a demo's scenes" lists what a scene is given.
- **The directions.** ` + "`[2s]`" + ` (how long the step below lasts; ` + "`[0s]`" + ` cuts it), ` + "`[speed 4x]`" + `, ` + "`[pause 2s]`" + `, ` + "`[cut]`" + ` and ` + "`[action]`" + ` (the camera stops and starts), ` + "`[card 3s] A title`" + `, ` + "`[show diagram]`" + `, ` + "`[hold response]`" + ` (the step shows what it sent, and what came back shows with the step after it). A comment line right below a cue, indented to its text, goes on with it.
- **Its sound.** Beside each film is a ` + "`.srt`" + ` file of its subtitles with their times. ` + "`soundtrack.command`" + ` names a command of the project's that reads it and writes the film's sound, a WAV file as long as the film (a model that speaks, say), which the film then has. The pack makes no sound itself.
- **Before an audience.** ` + "`axx demo --present`" + ` opens a page for the screen the audience sees, and the scenario runs as you go on, a subtitle at a time: forward shows the next and runs its steps, back shows again what was shown. A step that fails is not shown: you try it once more, play on from the last take, or start again from the top.
- **Your own look.** The stage is a web page: ` + "`theme`" + ` sets its colors and names, ` + "`styles`" + ` and ` + "`scenes`" + ` load a stylesheet and scripts of the project's own, and ` + "`directions`" + ` names directions of its own, which its scenes act on.
- **As a test.** ` + "`axx run`" + ` runs a demo's file as any feature. A demo does several things in turn, so the rule of one behavior per scenario is not for it.`

// session is the demo a command is making: the scenarios that run for it
// leave their takes in its folder. Outside one (any other run), the pack
// keeps nothing.
type session struct {
	dir string
	// feature is the demo's file, relative to the project.
	feature string
	takes   []string
	// cameras film the screens of the scenario that runs.
	cameras []*camera
	// stamp names the run's own folder of pictures, so that a run leaves
	// those of the take before it alone.
	stamp string
	// show is the show the run is for, before an audience; nil for a film.
	show *show
}

var (
	sessionMu sync.Mutex
	current   *session
)

func begin(s *session) {
	sessionMu.Lock()
	current = s
	sessionMu.Unlock()
	// The screens of the demo's scenarios are filmed as they run.
	recording.Watch(func(sc *core.Scenario) bool { return of(sc) != nil }, func(sc *core.Scenario, surface string, look recording.Look) {
		sessionMu.Lock()
		defer sessionMu.Unlock()
		if current != nil {
			current.cameras = append(current.cameras, film(sc, current.dir, current.stamp, surface, look))
		}
	})
}

func end() *session {
	recording.Watch(nil, nil)
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s := current
	current = nil
	return s
}

// of is the demo a scenario runs for, or nil.
func of(sc *core.Scenario) *session {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	if current == nil || filepath.ToSlash(sc.URI) != current.feature {
		return nil
	}
	return current
}

// camerasOf are the cameras of the demo a scenario runs for.
func camerasOf(s *session) []*camera {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return append([]*camera(nil), s.cameras...)
}

// pictureOf waits, after a step on a screen, for the screen's picture of
// what the step did, and shows an audience the step.
func pictureOf(sc *core.Scenario) error {
	s := of(sc)
	st := sc.Step()
	if s == nil || st == nil {
		return nil
	}
	cams := camerasOf(s)
	if len(cams) > 0 && surfaceOf(st.Pack) != "" {
		release := sc.Hold()
		settle(cams)
		release()
	}
	if s.show != nil {
		s.show.update(sc, cams)
	}
	return nil
}

// gate waits, before a step of a show, for the presenter to go on to it.
func gate(sc *core.Scenario) error {
	if s := of(sc); s != nil && s.show != nil {
		s.show.update(sc, camerasOf(s))
		s.show.gate(sc)
	}
	return nil
}

// stepFailed asks the presenter of a show what to do about a step that
// failed.
func stepFailed(sc *core.Scenario) error {
	if s := of(sc); s != nil && s.show != nil {
		return s.show.failed(sc)
	}
	return nil
}

// keepTake keeps the take of a scenario that ran for a demo.
func keepTake(sc *core.Scenario) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s := current
	if s == nil || filepath.ToSlash(sc.URI) != s.feature {
		return nil
	}
	take := takeOf(sc)
	for _, c := range s.cameras {
		take.Footage = append(take.Footage, c.stop()...)
	}
	s.cameras = nil
	sortFootage(take)
	path := filepath.Join(s.dir, fmt.Sprintf("take-%d.json", sc.Line))
	if s.show != nil && !take.Passed {
		// A show that failed leaves the last good take as it is: the show
		// may play on from it.
		path = filepath.Join(s.dir, fmt.Sprintf("failed-%d.json", sc.Line))
	}
	if err := writeTake(path, take); err != nil {
		return fmt.Errorf("cannot keep the demo's take: %w", err)
	}
	s.takes = append(s.takes, path)
	return nil
}

// cuesCheck is the pack's check of a demo's cues: a direction the pack does
// not know, or one written wrong.
func cuesCheck() core.FeatureCheck {
	return core.FeatureCheck{
		ID: "demo-cues", Name: "A demo's cues",
		Description: "A comment that starts with demo: in a *.demo.feature file directs the demo's film: its directions, in square brackets, are ones the demo pack or the project has.",
		Run: func(call *core.FeatureCheckCall) []core.FeatureFinding {
			var st Settings
			_ = call.Settings(&st)
			var out []core.FeatureFinding
			// Each file once, at its first scenario.
			seen := map[string]bool{}
			for _, sc := range call.Scenarios {
				if seen[sc.URI] || !strings.HasSuffix(sc.URI, Suffix) {
					continue
				}
				seen[sc.URI] = true
				src, err := os.ReadFile(filepath.Join(call.ProjectDir, filepath.FromSlash(sc.URI)))
				if err != nil {
					continue
				}
				script, err := ParseScript(sc.URI, src, st.known())
				if err != nil {
					continue
				}
				for _, p := range script.Problems {
					out = append(out, core.FeatureFinding{URI: sc.URI, Line: sc.Line, At: p.Line, Column: p.Column, Message: "the demo's cue: " + p.Message})
				}
			}
			return out
		},
	}
}
