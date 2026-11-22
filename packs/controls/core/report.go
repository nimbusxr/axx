package controlscore

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// State is how a control stands, by what covers it.
type State string

// The states of a control.
const (
	// Proven: the scenarios that claim it passed, as many as it takes.
	Proven State = "proven"
	// Covered: something other than a scenario covers it.
	Covered State = "covered"
	// Excepted: the project recorded an exception that still counts.
	Excepted State = "excepted"
	// PartlyProven: its scenarios passed, and it takes more of them.
	PartlyProven State = "partly proven"
	// Failing: a scenario that claims it did not pass, or another source
	// says it fails.
	Failing State = "failing"
	// NotCovered: nothing covers it.
	NotCovered State = "not covered"
)

// met reports whether the state is one a required control may be in.
func (s State) met() bool { return s == Proven || s == Covered || s == Excepted }

// Report is the controls of a project, each with what covers it: what `axx
// controls report` writes, in whatever format, and what `axx controls check`
// judges.
type Report struct {
	// Axx, Commit and Dirty are of the runs the report is of; Started and
	// Ended, their times.
	Axx     string    `json:"axx,omitempty"`
	Commit  string    `json:"commit,omitempty"`
	Dirty   bool      `json:"dirty,omitempty"`
	Started time.Time `json:"started,omitzero"`
	Ended   time.Time `json:"ended,omitzero"`
	// Partial says why the runs are not of every scenario the project's
	// configuration selects, when they are not: a control they leave not
	// covered may be one their scenarios did not get to.
	Partial string `json:"partial,omitempty"`
	// Lists are the lists the project requires.
	Lists []ReportList `json:"lists,omitempty"`
	// Controls are the required controls, in their lists' order, then the
	// controls that are covered and not required.
	Controls []ControlResult `json:"controls"`
	// Unknown are the tags that name a control its framework has not.
	Unknown []UnknownClaim `json:"unknown,omitempty"`
	// Sources are the sources other than runs whose findings are in the
	// report.
	Sources []OtherSource `json:"sources,omitempty"`
	// Requested are the project's own exceptions, when the report goes by
	// others: asked for, and not counted.
	Requested []Exception `json:"requested,omitempty"`
	// Notices are the terms of the frameworks whose text the report may
	// show.
	Notices []string `json:"-"`
}

// ReportList is a list a project requires.
type ReportList struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	// Framework is the key of the framework its controls are of; Of, that
	// framework's title and version ("OWASP ASVS 5.0.0").
	Framework string `json:"framework"`
	Version   string `json:"version,omitempty"`
	Of        string `json:"of,omitempty"`
	Controls  int    `json:"controls"`
}

// ControlResult is a control, with what covers it.
type ControlResult struct {
	// Control is the control as a tag names it without its @, asvs:6.2.1;
	// Framework and ID, its two parts.
	Control   string `json:"control"`
	Framework string `json:"framework"`
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	Group     string `json:"group,omitempty"`
	// Text is what the control requires, in its framework's words: for the
	// reports a person reads, never written into evidence.
	Text string `json:"-"`
	// Required is whether a list the project requires has it, and List
	// which; Needs, how many passing scenarios it takes when more than one.
	Required bool   `json:"required"`
	List     string `json:"list,omitempty"`
	Needs    int    `json:"needs,omitempty"`
	State    State  `json:"state"`
	// Scenarios are the scenarios that claim it.
	Scenarios []ScenarioRef `json:"scenarios,omitempty"`
	// Other is what sources other than runs say of it.
	Other []OtherFinding `json:"other,omitempty"`
	// Exception is the exception recorded for it, whether it still counts
	// (Expired) or not.
	Exception *Exception `json:"exception,omitempty"`
	Expired   bool       `json:"expired,omitempty"`
}

// ScenarioRef is a scenario that claims a control.
type ScenarioRef struct {
	URI    string `json:"uri"`
	Line   int    `json:"line"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// OtherFinding is what a source other than a run says of a control.
type OtherFinding struct {
	Source string `json:"source"`
	Link   string `json:"link,omitempty"`
	Date   string `json:"date,omitempty"`
	State  State  `json:"state"`
	Note   string `json:"note,omitempty"`
}

// OtherSource is a source other than a run.
type OtherSource struct {
	Source   string `json:"source"`
	Link     string `json:"link,omitempty"`
	Date     string `json:"date,omitempty"`
	Controls int    `json:"controls"`
}

// UnknownClaim is a tag that names a control its framework has not.
type UnknownClaim struct {
	// Tag is the tag, with its @; Nearest, the control of its framework
	// closest to it, as a tag.
	Tag     string `json:"tag"`
	Nearest string `json:"nearest,omitempty"`
	// Scenarios are the scenarios that have the tag.
	Scenarios []ScenarioRef `json:"scenarios"`
}

// Counts are how many of a report's required controls are in each state.
type Counts struct {
	Required   int `json:"required"`
	Proven     int `json:"proven"`
	Covered    int `json:"covered"`
	Excepted   int `json:"excepted"`
	Partly     int `json:"partlyProven"`
	Failing    int `json:"failing"`
	NotCovered int `json:"notCovered"`
}

// Met is how many required controls are covered one way or another.
func (c Counts) Met() int { return c.Proven + c.Covered + c.Excepted }

// Counts tallies the required controls by state.
func (r *Report) Counts() Counts {
	var c Counts
	for _, cr := range r.Controls {
		if !cr.Required {
			continue
		}
		c.Required++
		switch cr.State {
		case Proven:
			c.Proven++
		case Covered:
			c.Covered++
		case Excepted:
			c.Excepted++
		case PartlyProven:
			c.Partly++
		case Failing:
			c.Failing++
		default:
			c.NotCovered++
		}
	}
	return c
}

// Unmet are the required controls that are not covered.
func (r *Report) Unmet() []ControlResult {
	var out []ControlResult
	for _, cr := range r.Controls {
		if cr.Required && !cr.State.met() {
			out = append(out, cr)
		}
	}
	return out
}

// claims are what a run's scenario claims of the project's frameworks: the
// controls its tags name, each written as its framework writes it, and the
// tags of a framework the project has whose control the framework has not.
func (p *project) claims(tags []string) (controls, unknown []string) {
	for _, tag := range tags {
		key, id, ok := splitRef(tag)
		f := p.frameworks[key]
		if !ok || f == nil {
			continue
		}
		c, known := f.control(id)
		if !known {
			unknown = append(unknown, key+":"+id)
			continue
		}
		controls = append(controls, key+":"+c.ID)
	}
	return controls, unknown
}

// report works out how each control stands, from the runs' evidence (merged
// into one) and what other sources say. at is the time exceptions are judged
// at.
func (p *project) report(run *Evidence, others []*Evidence, at time.Time) *Report {
	r := &Report{Requested: p.requested}
	if run != nil {
		r.Axx, r.Commit, r.Dirty, r.Started, r.Ended, r.Partial = run.Axx, run.Commit, run.Dirty, run.Started, run.Ended, run.Partial
		if !run.Ended.IsZero() {
			at = run.Ended
		}
	}

	// Who claims what.
	claimed := map[string][]ScenarioRef{}
	unknown := map[string]*UnknownClaim{}
	var unknownOrder []string
	if run != nil {
		for _, sc := range run.Scenarios {
			ref := ScenarioRef{URI: sc.URI, Line: sc.Line, Name: sc.Name, Status: sc.Status}
			// What the evidence names is looked up again: the frameworks are
			// the project's as it is now.
			controls, lost := p.claims(prefixed(sc.Controls))
			for _, c := range controls {
				claimed[c] = append(claimed[c], ref)
			}
			for _, u := range append(lost, sc.Unknown...) {
				tag := "@" + strings.TrimPrefix(u, "@")
				uc := unknown[tag]
				if uc == nil {
					uc = &UnknownClaim{Tag: tag}
					if key, id, ok := splitRef(tag); ok && p.frameworks[key] != nil {
						if near := p.frameworks[key].nearest(id); near != "" {
							uc.Nearest = "@" + key + ":" + near
						}
					}
					unknown[tag] = uc
					unknownOrder = append(unknownOrder, tag)
				}
				uc.Scenarios = append(uc.Scenarios, ref)
			}
		}
	}
	for _, tag := range unknownOrder {
		r.Unknown = append(r.Unknown, *unknown[tag])
	}

	// What other sources say.
	found := map[string][]OtherFinding{}
	for _, o := range others {
		n := 0
		for _, f := range o.Findings {
			key, id, ok := splitRef(f.Control)
			fw := p.frameworks[key]
			if !ok || fw == nil {
				continue
			}
			c, known := fw.control(id)
			if !known {
				continue
			}
			ref := key + ":" + c.ID
			found[ref] = append(found[ref], OtherFinding{Source: o.Source, Link: o.Link, Date: o.Date, State: State(f.State), Note: f.Note})
			n++
		}
		r.Sources = append(r.Sources, OtherSource{Source: o.Source, Link: o.Link, Date: o.Date, Controls: n})
	}

	excepted := map[string]Exception{}
	for _, e := range p.exceptions {
		excepted[e.Control] = e
	}

	result := func(f *Framework, c *Control, ref string) ControlResult {
		cr := ControlResult{
			Control: ref, Framework: f.Key, ID: c.ID, Title: c.Title, Group: c.Group, Text: c.Text,
			Scenarios: claimed[ref], Other: found[ref],
		}
		if e, ok := excepted[ref]; ok {
			cr.Exception, cr.Expired = &e, !e.in(at)
		}
		return cr
	}

	listed := map[string]bool{}
	notices := map[string]bool{}
	for _, l := range p.lists {
		f := l.Framework
		r.Lists = append(r.Lists, ReportList{Name: l.Name, Title: l.Title, Framework: f.Key, Version: f.Version, Of: describe(f), Controls: len(l.Controls)})
		if f.Notice != "" && !notices[f.Notice] {
			notices[f.Notice] = true
			r.Notices = append(r.Notices, f.Notice)
		}
		for _, req := range l.Controls {
			c, ok := f.control(req.ID)
			if !ok {
				continue
			}
			ref := f.Key + ":" + c.ID
			if listed[ref] {
				continue
			}
			listed[ref] = true
			cr := result(f, c, ref)
			cr.Required, cr.List, cr.Needs = true, l.Name, req.Scenarios
			cr.State = stateOf(cr)
			r.Controls = append(r.Controls, cr)
		}
	}
	// What is covered and not required, in the frameworks' order.
	var extra []string
	for ref := range claimed {
		if !listed[ref] {
			extra = append(extra, ref)
		}
	}
	for ref := range found {
		if !listed[ref] && claimed[ref] == nil {
			extra = append(extra, ref)
		}
	}
	sort.Strings(extra)
	for _, ref := range extra {
		key, id, _ := splitRef(ref)
		f := p.frameworks[key]
		c, ok := f.control(id)
		if !ok {
			continue
		}
		cr := result(f, c, ref)
		cr.State = stateOf(cr)
		r.Controls = append(r.Controls, cr)
	}
	return r
}

// prefixed puts the @ of a tag back on the controls an evidence names.
func prefixed(controls []string) []string {
	out := make([]string, len(controls))
	for i, c := range controls {
		out[i] = "@" + strings.TrimPrefix(c, "@")
	}
	return out
}

// stateOf is how a control stands. What fails comes first: a scenario that
// claims it and did not pass, or a source that says it fails. Then what proves
// it, what else covers it, and an exception that still counts.
func stateOf(cr ControlResult) State {
	passing := 0
	for _, sc := range cr.Scenarios {
		switch sc.Status {
		case "passed":
			passing++
		case "skipped":
		default:
			return Failing
		}
	}
	covered := false
	for _, o := range cr.Other {
		if o.State == Failing {
			return Failing
		}
		covered = covered || o.State == Covered
	}
	needs := max(cr.Needs, 1)
	switch {
	case passing >= needs:
		return Proven
	case covered:
		return Covered
	case cr.Exception != nil && !cr.Expired:
		return Excepted
	case passing > 0:
		return PartlyProven
	}
	return NotCovered
}

// why says, in a few words, what a control's state rests on.
func (cr ControlResult) why() string {
	switch cr.State {
	case Proven:
		return plural(passing(cr.Scenarios), "scenario")
	case Covered:
		var by []string
		for _, o := range cr.Other {
			if o.State == Covered {
				by = append(by, o.Source)
			}
		}
		return strings.Join(by, "; ")
	case Excepted:
		if cr.Exception.Kind == Accepted {
			return fmt.Sprintf("accepted until %s: %s", cr.Exception.Until, cr.Exception.Reason)
		}
		return "does not apply: " + cr.Exception.Reason
	case PartlyProven:
		return fmt.Sprintf("%s of the %d it takes", plural(passing(cr.Scenarios), "scenario"), cr.Needs)
	case Failing:
		for _, sc := range cr.Scenarios {
			if sc.Status != "passed" && sc.Status != "skipped" {
				return fmt.Sprintf("%s:%d %s", sc.URI, sc.Line, sc.Status)
			}
		}
		for _, o := range cr.Other {
			if o.State == Failing {
				if o.Note != "" {
					return o.Source + ": " + o.Note
				}
				return o.Source
			}
		}
	}
	if cr.Exception != nil && cr.Expired {
		return fmt.Sprintf("its exception ended on %s", cr.Exception.Until)
	}
	return "no scenario is tagged @" + cr.Control
}

func passing(scenarios []ScenarioRef) int {
	n := 0
	for _, sc := range scenarios {
		if sc.Status == "passed" {
			n++
		}
	}
	return n
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}
