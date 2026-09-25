// Package runner executes pickles against registered step definitions.
package runner

import (
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Status is a step or scenario outcome. Higher values are "worse"; a
// scenario's status is the worst status of its steps and hooks.
type Status int

const (
	Passed Status = iota
	Skipped
	Pending
	Undefined
	Ambiguous
	Failed
)

func (s Status) String() string {
	switch s {
	case Passed:
		return "passed"
	case Skipped:
		return "skipped"
	case Pending:
		return "pending"
	case Undefined:
		return "undefined"
	case Ambiguous:
		return "ambiguous"
	case Failed:
		return "failed"
	}
	return "unknown"
}

// MarshalText implements encoding.TextMarshaler.
func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Attachment is a log line or file attached to a step.
type Attachment struct {
	MediaType string `json:"mediaType"`
	Name      string `json:"name,omitempty"`
	Body      []byte `json:"-"`
}

// StepResult is the outcome of one step (or hook).
type StepResult struct {
	// Hook is set for hook results (and Text is the hook ID).
	Hook       *core.Hook      `json:"-"`
	Keyword    string          `json:"keyword,omitempty"`
	Text       string          `json:"text"`
	Line       int             `json:"line,omitempty"`
	Background bool            `json:"background,omitempty"`
	Table      *core.Table     `json:"dataTable,omitempty"`
	DocString  *core.DocString `json:"docString,omitempty"`
	Status     Status          `json:"status"`
	Duration   time.Duration   `json:"duration"`
	Err        error           `json:"-"`
	// Match is the matched definition (nil when undefined/ambiguous).
	Match *match.Match `json:"-"`
	// Candidates lists the matching expressions of an ambiguous step.
	Candidates []string `json:"candidates,omitempty"`
	// Suggestions lists close expressions for an undefined step.
	Suggestions []match.Suggestion `json:"suggestions,omitempty"`
	Logs        []string           `json:"logs,omitempty"`
	Attachments []Attachment       `json:"attachments,omitempty"`
	// PickleStepID links the result to its pickle step.
	PickleStepID string `json:"-"`
}

// ScenarioResult is the outcome of one pickle.
type ScenarioResult struct {
	Pickle   *feature.Pickle
	Status   Status
	Before   []*StepResult
	Steps    []*StepResult
	After    []*StepResult
	Started  time.Time
	Duration time.Duration
	Attempt  int
	// Worker is the worker that ran the scenario (0 for the serial phase).
	Worker int
	// Context holds pack-provided diagnostic context captured on failure
	// (e.g. the last request/response), keyed by pack name.
	Context map[string]any
}

// FirstFailure returns the first failing step or hook, if any.
func (r *ScenarioResult) FirstFailure() *StepResult {
	for _, group := range [][]*StepResult{r.Before, r.Steps, r.After} {
		for _, s := range group {
			if s.Status == Failed || s.Status == Ambiguous || s.Status == Undefined || s.Status == Pending {
				return s
			}
		}
	}
	return nil
}

// RunResult summarizes a run.
type RunResult struct {
	Scenarios []*ScenarioResult
	// RunErrors are problems found after the scenarios, outside any of them
	// (core.Finisher); any of them fails the run.
	RunErrors []RunError
	Started   time.Time
	Duration  time.Duration
	// NotRun counts scenarios skipped by fail-fast or interruption.
	NotRun      int
	Interrupted bool
	DryRun      bool
}

// Counts tallies scenario and step statuses.
type Counts struct {
	Scenarios map[Status]int `json:"scenarios"`
	Steps     map[Status]int `json:"steps"`
}

// Counts returns status tallies.
func (r *RunResult) Counts() Counts {
	c := Counts{Scenarios: map[Status]int{}, Steps: map[Status]int{}}
	for _, s := range r.Scenarios {
		c.Scenarios[s.Status]++
		for _, st := range s.Steps {
			c.Steps[st.Status]++
		}
	}
	return c
}

// Worst returns the worst scenario status.
func (r *RunResult) Worst() Status {
	w := Passed
	for _, s := range r.Scenarios {
		w = max(w, s.Status)
	}
	if len(r.RunErrors) > 0 {
		w = max(w, Failed)
	}
	return w
}

// RunError is a problem found after the scenarios ran, reported by a pack.
type RunError struct {
	// Source is the pack that reported it.
	Source  string
	Message string
}

// worst is the worst status of the scenario's hooks and steps so far.
func (r *ScenarioResult) worst() Status {
	st := Passed
	for _, group := range [][]*StepResult{r.Before, r.Steps, r.After} {
		for _, s := range group {
			st = max(st, s.Status)
		}
	}
	return st
}
