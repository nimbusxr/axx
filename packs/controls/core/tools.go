package controlscore

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// tools are what the controls-core pack lets coding agents do through `axx
// mcp`: find out which required controls are still to be covered, and what one
// requires, in few words. An agent reads no report made for a person.
func tools() []core.Tool {
	return []core.Tool{{
		Name: "controls_status",
		Description: "The controls this project is required to cover that are not covered yet, each with a line of what it requires, " +
			"as the last run of the scenarios left them. A scenario covers a control by proving what it requires and carrying its tag (@asvs:6.2.1). " +
			"Use it before writing scenarios for a feature that touches security, and after a run, to see what is left. " +
			"Never tag a scenario with a control it does not prove, and never record an exception: that is a person's to decide.",
		ReadOnly: true,
		Input: json.RawMessage(`{
  "type": "object",
  "properties": {
    "words": {"type": "string", "description": "only the controls that mention these words, like \"password\" or \"session\""}
  },
  "additionalProperties": false
}`),
		Run: status,
	}, {
		Name: "controls_show",
		Description: "One control: what it requires, in its framework's words, how it stands, and the scenarios that claim it. " +
			"Name it as a tag does, without the @: asvs:6.2.1.",
		ReadOnly: true,
		Input: json.RawMessage(`{
  "type": "object",
  "properties": {
    "control": {"type": "string", "description": "the control, as a tag names it: asvs:6.2.1"}
  },
  "required": ["control"],
  "additionalProperties": false
}`),
		Run: show,
	}}
}

// maxStatus is how many controls controls_status names: an agent asks again
// with words for the rest.
const maxStatus = 25

// statusView is what controls_status says.
type statusView struct {
	Summary string `json:"summary"`
	// Judged is false when no run has written its evidence yet.
	Judged bool `json:"judged"`
	// ToCover are the required controls that are not covered.
	ToCover []statusControl `json:"toCover,omitempty"`
	More    int             `json:"more,omitempty"`
	// UnknownTags are the tags that name a control its framework has not.
	UnknownTags []string `json:"unknownTags,omitempty"`
}

type statusControl struct {
	Control string `json:"control"`
	State   State  `json:"state"`
	Why     string `json:"why"`
	// Requires is what the control requires, in a line.
	Requires string `json:"requires,omitempty"`
}

func projectOfScenario(sc *core.Scenario) (*project, error) {
	s := sc.Suite()
	return loadProject(s.ProjectDir(), settingsOf(s), overrides{})
}

func status(call *core.ToolCall) (*core.ToolResult, error) {
	var in struct {
		Words string `json:"words"`
	}
	if len(call.Input) > 0 {
		if err := json.Unmarshal(call.Input, &in); err != nil {
			return nil, err
		}
	}
	p, err := projectOfScenario(call.Scenario)
	if err != nil {
		return nil, err
	}
	r, judged, err := p.judge(nil)
	if err != nil {
		return nil, err
	}
	view := statusView{Summary: headline(r), Judged: judged}
	words := strings.Fields(strings.ToLower(in.Words))
	for _, cr := range r.Unmet() {
		hay := strings.ToLower(cr.Control + " " + cr.Title + " " + cr.Group + " " + cr.Text)
		match := true
		for _, w := range words {
			match = match && strings.Contains(hay, w)
		}
		if !match {
			continue
		}
		if len(view.ToCover) == maxStatus {
			view.More++
			continue
		}
		view.ToCover = append(view.ToCover, statusControl{Control: cr.Control, State: cr.State, Why: cr.why(), Requires: oneLine(cr.Text, cr.Title, 200)})
	}
	for _, u := range r.Unknown {
		tag := u.Tag
		if u.Nearest != "" {
			tag += fmt.Sprintf(" (the nearest control is %s)", u.Nearest)
		}
		view.UnknownTags = append(view.UnknownTags, tag)
	}
	return &core.ToolResult{Data: view}, nil
}

// controlView is what controls_show says.
type controlView struct {
	Control  string `json:"control"`
	Title    string `json:"title,omitempty"`
	Requires string `json:"requires,omitempty"`
	Required bool   `json:"required"`
	// Needs is how many passing scenarios it takes, when more than one.
	Needs     int           `json:"needs,omitempty"`
	State     State         `json:"state"`
	Why       string        `json:"why"`
	Scenarios []ScenarioRef `json:"scenarios,omitempty"`
	Tag       string        `json:"tag"`
}

func show(call *core.ToolCall) (*core.ToolResult, error) {
	var in struct {
		Control string `json:"control"`
	}
	if err := json.Unmarshal(call.Input, &in); err != nil {
		return nil, err
	}
	p, err := projectOfScenario(call.Scenario)
	if err != nil {
		return nil, err
	}
	key, id, ok := splitRef(in.Control)
	f := p.frameworks[key]
	if !ok || f == nil {
		return nil, fmt.Errorf("%q names a control of no framework this project has: name it as a tag does, asvs:6.2.1", in.Control)
	}
	c, known := f.control(id)
	if !known {
		if near := f.nearest(id); near != "" {
			return nil, fmt.Errorf("%s has no control %s: the nearest is %s:%s", describe(f), id, key, near)
		}
		return nil, fmt.Errorf("%s has no control %s", describe(f), id)
	}
	ref := key + ":" + c.ID
	r, _, err := p.judge(nil)
	if err != nil {
		return nil, err
	}
	view := controlView{Control: ref, Title: c.Title, Requires: strings.Join(strings.Fields(c.Text), " "), State: NotCovered, Tag: "@" + ref}
	view.Why = "no scenario is tagged @" + ref
	for _, cr := range r.Controls {
		if cr.Control == ref {
			view.Required, view.Needs, view.State, view.Why, view.Scenarios = cr.Required, cr.Needs, cr.State, cr.why(), cr.Scenarios
		}
	}
	return &core.ToolResult{Data: view}, nil
}
