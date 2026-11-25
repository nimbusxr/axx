package demo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
)

// A take is the record of one run of a demo's scenario: every step with
// when it ran and how it ended, and what it sent to and saw of what the
// scenario talks to. The video is made from it, and can be made again from it
// without another run; before an audience, the last good take is what the
// show falls back on.

// TakeVersion is the version of the take's format.
const TakeVersion = 1

// Take is one run of a scenario, as the pack recorded it.
type Take struct {
	Version int `json:"version"`
	// Feature is the feature file, relative to the project, and Scenario
	// and Line the scenario that ran: of an outline, the row's line.
	Feature  string `json:"feature"`
	Scenario string `json:"scenario"`
	Line     int    `json:"line"`
	// At is when the scenario started, and Passed whether every step passed.
	At     time.Time `json:"at"`
	Passed bool      `json:"passed"`
	// Peers are what the scenario talked to, in the order it first did.
	Peers []Peer     `json:"peers"`
	Steps []TakeStep `json:"steps"`
	// Footage are the pictures of the scenario's screens, in the order they
	// were taken: a screen's picture shows until its next.
	Footage []Shot `json:"footage,omitempty"`
}

// Shot is a picture of a screen, at a moment of the run.
type Shot struct {
	// Surface is the screen: "browser", "phone", "desktop".
	Surface string `json:"surface"`
	// File is the picture, a file beside the take.
	File string `json:"file"`
	// At is when it was taken, in seconds from the scenario's start.
	At float64 `json:"at"`
}

// Peer is something the scenario talked to: a service, a database, a topic,
// a mock.
type Peer struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// TakeStep is one step of a take.
type TakeStep struct {
	Keyword    string `json:"keyword"`
	Text       string `json:"text"`
	Line       int    `json:"line"`
	Pack       string `json:"pack,omitempty"`
	Definition string `json:"definition,omitempty"`
	Background bool   `json:"background,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	// Start and End are when it ran, in seconds from the scenario's start.
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	// Settled is when the camera had the picture of what it did on a
	// screen, after it ended: what the screen showed until then is the
	// step's doing. 0 for a step on no screen.
	Settled float64 `json:"settled,omitempty"`
	// Exchanges are what it sent and saw.
	Exchanges []core.Exchange `json:"exchanges,omitempty"`
}

// takeOf is the take of a scenario that ran, from what it kept of its steps.
func takeOf(sc *core.Scenario) *Take {
	t := &Take{
		Version: TakeVersion, Feature: filepath.ToSlash(sc.URI), Scenario: sc.Name, Line: sc.Line,
		At: sc.Started(), Passed: true, Peers: []Peer{},
	}
	interpolate := sc.Suite().Interpolate
	steps := sc.Progress()
	byLine := map[int]int{}
	for i, sp := range steps {
		st := TakeStep{
			Keyword: sp.Keyword, Text: sysRef.ReplaceAllStringFunc(sp.Text, interpolate), Line: sp.Line,
			Pack: sp.Pack, Definition: sp.Definition, Background: sp.Background,
			Status: sp.Status, Error: sp.Error,
		}
		if !sp.Started.IsZero() {
			st.Start = sp.Started.Sub(t.At).Seconds()
			st.End = st.Start
			if !sp.Ended.IsZero() {
				st.End = sp.Ended.Sub(t.At).Seconds()
			}
		}
		st.Settled = settledAt(sc, sp.Line)
		// A step of a show waits for the presenter before it runs: its time
		// begins when it goes on.
		if began := beganAt(sc, sp.Line); began > st.Start {
			st.Start = began
			st.End = max(st.End, st.Start)
		}
		if sp.Status != "passed" {
			t.Passed = false
		}
		t.Steps = append(t.Steps, st)
		byLine[sp.Line] = i
	}
	seen := map[string]bool{}
	for _, x := range sc.Exchanges() {
		if x.Peer != "" && !seen[x.Peer] {
			seen[x.Peer] = true
			t.Peers = append(t.Peers, Peer{Name: x.Peer, Kind: x.Kind})
		}
		if i, ok := byLine[x.Line]; ok {
			t.Steps[i].Exchanges = append(t.Steps[i].Exchanges, x)
		}
	}
	return t
}

// sysRef is a reference to a property in a step's text, which the take
// fills in: what a step said is what the video shows. An environment
// variable's stays as written, as it may be a secret.
var sysRef = regexp.MustCompile(`\$\{sys:[^}]+\}`)

// takeDir is where a demo's take and video are kept: a folder of the
// project's .axx/demo, named after the feature file.
func takeDir(projectDir, featureURI string) string {
	name := strings.TrimSuffix(filepath.Base(featureURI), ".feature")
	name = strings.TrimSuffix(name, ".demo")
	return filepath.Join(projectDir, ".axx", "demo", slug(name))
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.Trim(notSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "demo"
	}
	return s
}

func writeTake(path string, t *Take) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func readTake(path string) (*Take, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Take
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("%s is no take: %w", path, err)
	}
	if t.Version != TakeVersion {
		return nil, fmt.Errorf("%s is a take of version %d, and this axx reads version %d: run the demo again", path, t.Version, TakeVersion)
	}
	return &t, nil
}
