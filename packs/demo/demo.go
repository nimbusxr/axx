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
	"strconv"
	"strings"
	"sync"

	"github.com/nimbusxr/axx/core"
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
		Hooks: []core.Hook{{
			ID: "demo.take", Phase: core.AfterScenario, Order: -1000,
			Run: keepTake,
		}},
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
- **What it shows.** A step on a screen shows the screen. Any other step shows a diagram: what the scenario talks to as boxes, and what the step sent and what came back, as the run saw it.
- **The directions.** ` + "`[2s]`" + ` (how long the step below lasts; ` + "`[0s]`" + ` cuts it), ` + "`[speed 4x]`" + `, ` + "`[pause 2s]`" + `, ` + "`[cut]`" + ` and ` + "`[action]`" + ` (the camera stops and starts), ` + "`[card 3s] A title`" + `, ` + "`[show diagram]`" + `. A comment line right below a cue, indented to its text, goes on with it.
- **As a test.** ` + "`axx run`" + ` runs a demo's file as any feature. A demo does several things in turn, so the rule of one behavior per scenario is not for it.`

// session is the demo a command is making: the scenarios that run for it
// leave their takes in its folder. Outside one (any other run), the pack
// keeps nothing.
type session struct {
	dir string
	// feature is the demo's file, relative to the project.
	feature string
	takes   []string
}

var (
	sessionMu sync.Mutex
	current   *session
)

func begin(s *session) {
	sessionMu.Lock()
	current = s
	sessionMu.Unlock()
}

func end() *session {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s := current
	current = nil
	return s
}

// keepTake keeps the take of a scenario that ran for a demo.
func keepTake(sc *core.Scenario) error {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	s := current
	if s == nil || filepath.ToSlash(sc.URI) != s.feature {
		return nil
	}
	path := filepath.Join(s.dir, fmt.Sprintf("take-%d.json", sc.Line))
	if err := writeTake(path, takeOf(sc)); err != nil {
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
				script, err := ParseScript(sc.URI, src, st.Directions)
				if err != nil {
					continue
				}
				for _, p := range script.Problems {
					out = append(out, core.FeatureFinding{URI: sc.URI, Line: sc.Line, Message: "the demo's cue on line " + strconv.Itoa(p.Line) + ": " + p.Message})
				}
			}
			return out
		},
	}
}
