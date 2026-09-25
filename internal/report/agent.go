package report

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// AgentSchemaVersion is the version of the agent report contract. Adding
// fields is non-breaking; removing or retyping fields requires a new version.
const AgentSchemaVersion = 1

// Limits that keep agent reports small.
const (
	agentMaxLogs     = 50
	agentMaxLogRunes = 2000
)

// AgentReport is the document written by the agent reporter: one JSON
// object describing the whole run, designed for coding agents.
type AgentReport struct {
	SchemaVersion int `json:"schemaVersion"`
	// Axx is the axx version that produced the report.
	Axx string `json:"axx"`
	// Result is "interrupted" when the run was interrupted, otherwise the
	// worst scenario status: passed, skipped, pending, undefined, ambiguous
	// or failed.
	Result      string      `json:"result"`
	DurationMs  int64       `json:"durationMs"`
	Counts      AgentCounts `json:"counts"`
	NotRun      int         `json:"notRun"`
	Interrupted bool        `json:"interrupted"`
	DryRun      bool        `json:"dryRun,omitempty"`
	// Failures has one entry per failed, undefined, ambiguous or pending
	// scenario, in feature file order.
	Failures []AgentFailure `json:"failures"`
	// Undefined lists each undefined step once per location.
	Undefined []AgentUndefined `json:"undefined"`
	// RunErrors are problems found after the scenarios, outside any of them
	// (e.g. contract violations on a mocked dependency no scenario checked).
	// Any of them makes the result "failed".
	RunErrors []AgentRunError `json:"runErrors,omitempty"`
}

// AgentRunError is a problem a pack found after the scenarios ran.
type AgentRunError struct {
	// Source is the pack that reported it.
	Source  string `json:"source"`
	Message string `json:"message"`
}

// AgentCounts tallies scenarios and steps by status (zero counts omitted).
type AgentCounts struct {
	Scenarios map[string]int `json:"scenarios"`
	Steps     map[string]int `json:"steps"`
}

// AgentFailure describes one non-passing scenario.
type AgentFailure struct {
	Scenario string `json:"scenario"`
	// Location is "<uri>:<line>" (the example row line for outline rows).
	Location string   `json:"location"`
	Status   string   `json:"status"`
	Tags     []string `json:"tags,omitempty"`
	// Step is the step or hook responsible for the status.
	Step  *AgentStep  `json:"step,omitempty"`
	Error *AgentError `json:"error,omitempty"`
	// Logs are the scenario's step and hook logs, oldest first (capped).
	Logs []string `json:"logs,omitempty"`
	// Context is the diagnostic context packs captured on failure (e.g. the
	// last HTTP exchange), keyed by pack name.
	Context map[string]json.RawMessage `json:"context,omitempty"`
	// Rerun is the command that reruns just this scenario.
	Rerun string `json:"rerun,omitempty"`
}

// AgentStep identifies a step or hook.
type AgentStep struct {
	Keyword string `json:"keyword,omitempty"`
	Text    string `json:"text"`
	// Location is "<uri>:<line>"; empty for hooks.
	Location string `json:"location,omitempty"`
	// Definition is the matched step definition ID (or the hook ID).
	Definition string `json:"definition,omitempty"`
	// Hook is "before" or "after" for scenario hooks.
	Hook string `json:"hook,omitempty"`
}

// AgentError describes why a step did not pass.
type AgentError struct {
	// Kind is one of assertion, error, timeout, panic, undefined, ambiguous
	// or pending.
	Kind    string `json:"kind"`
	Message string `json:"message"`
	// Expected and Actual are the assertion's values as JSON.
	Expected json.RawMessage `json:"expected,omitempty"`
	Actual   json.RawMessage `json:"actual,omitempty"`
	// Diff is a unified line diff (-expected +actual) when both values are
	// JSON documents or multi-line strings.
	Diff []string `json:"diff,omitempty"`
	// Stack is the goroutine stack of a panic.
	Stack string `json:"stack,omitempty"`
	// Candidates are the matching definitions of an ambiguous step.
	Candidates []string `json:"candidates,omitempty"`
	// Suggestions are close step expressions for an undefined step.
	Suggestions []string `json:"suggestions,omitempty"`
}

// AgentUndefined is an undefined step with suggested expressions.
type AgentUndefined struct {
	Keyword     string   `json:"keyword,omitempty"`
	Text        string   `json:"text"`
	Location    string   `json:"location"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// agent writes an AgentReport on Finish.
type agent struct {
	out  *writer
	opts Options
}

func (a *agent) Envelope(*messages.Envelope) {}

func (a *agent) ScenarioFinished(*runner.ScenarioResult) {}

func (a *agent) Finish(res *runner.RunResult) error {
	b, err := marshalNoEscape(a.build(res))
	if err != nil {
		return err
	}
	a.out.put(string(b) + "\n")
	return a.out.err
}

func (a *agent) build(res *runner.RunResult) *AgentReport {
	rep := &AgentReport{
		SchemaVersion: AgentSchemaVersion,
		Axx:           a.opts.Version,
		Result:        res.Worst().String(),
		DurationMs:    res.Duration.Milliseconds(),
		NotRun:        res.NotRun,
		Interrupted:   res.Interrupted,
		DryRun:        res.DryRun,
		Failures:      []AgentFailure{},
		Undefined:     []AgentUndefined{},
	}
	if res.Interrupted {
		rep.Result = "interrupted"
	}
	c := res.Counts()
	rep.Counts = AgentCounts{Scenarios: statusMap(c.Scenarios), Steps: statusMap(c.Steps)}
	for _, re := range res.RunErrors {
		rep.RunErrors = append(rep.RunErrors, AgentRunError{Source: re.Source, Message: re.Message})
	}
	seen := map[string]bool{}
	for _, r := range res.Scenarios {
		if failing(r.Status) {
			rep.Failures = append(rep.Failures, a.failure(r))
		}
		for _, s := range r.Steps {
			if s.Status != runner.Undefined {
				continue
			}
			where := a.opts.stepLoc(r, s)
			if where == "" {
				where = a.opts.scenarioLoc(r)
			}
			if key := where + "\x00" + s.Text; !seen[key] {
				seen[key] = true
				rep.Undefined = append(rep.Undefined, AgentUndefined{
					Keyword: s.Keyword, Text: s.Text, Location: where, Suggestions: suggestionExprs(s),
				})
			}
		}
	}
	return rep
}

func (a *agent) failure(r *runner.ScenarioResult) AgentFailure {
	f := AgentFailure{
		Scenario: scenarioName(r), Location: a.opts.scenarioLoc(r), Status: r.Status.String(),
		Rerun: a.opts.rerun(r),
	}
	if r.Pickle != nil {
		f.Tags = r.Pickle.TagNames
	}
	if s := culprit(r); s != nil {
		f.Step = &AgentStep{Keyword: s.Keyword, Text: s.Text, Location: a.opts.stepLoc(r, s)}
		switch p := phaseOf(r, s); {
		case p == phaseBefore:
			f.Step.Hook = "before"
		case p == phaseAfter:
			f.Step.Hook = "after"
		case s.Match != nil:
			f.Step.Definition = s.Match.Def().Step.ID
		}
		if s.Hook != nil {
			f.Step.Definition = s.Hook.ID
		}
		if d := describe(s); d != nil {
			f.Error = &AgentError{Kind: d.kind, Message: d.message, Stack: d.stack, Candidates: d.candidates}
			if d.kind == KindUndefined {
				f.Error.Message = "undefined step: " + s.Text
				f.Error.Suggestions = suggestionExprs(s)
			}
			if d.values {
				f.Error.Expected = rawJSON(d.expected)
				f.Error.Actual = rawJSON(d.actual)
				f.Error.Diff = diffValues(d.expected, d.actual, 1)
			}
		}
	}
	allSteps(r, func(s *runner.StepResult, _ phase) {
		for _, l := range s.Logs {
			f.Logs = append(f.Logs, truncateRunes(stripANSI(l), agentMaxLogRunes))
		}
	})
	if len(f.Logs) > agentMaxLogs {
		f.Logs = f.Logs[len(f.Logs)-agentMaxLogs:]
	}
	if len(r.Context) > 0 {
		f.Context = map[string]json.RawMessage{}
		for k, v := range r.Context {
			f.Context[k] = rawJSON(v)
		}
	}
	return f
}

func statusMap(m map[runner.Status]int) map[string]int {
	out := map[string]int{}
	for st, n := range m {
		if n > 0 {
			out[st.String()] = n
		}
	}
	return out
}

func suggestionExprs(s *runner.StepResult) []string {
	var out []string
	for _, sg := range s.Suggestions {
		out = append(out, sg.Expr)
	}
	return out
}

// rawJSON encodes v verbatim when it is JSON-encodable, else as its %v
// string, so one odd value can never break the report. A nil value encodes
// as JSON null.
func rawJSON(v any) json.RawMessage {
	if b, ok := v.([]byte); ok && utf8.Valid(b) {
		v = string(b)
	}
	if b, err := marshalNoEscape(v); err == nil {
		return b
	}
	b, _ := marshalNoEscape(fmt.Sprintf("%v", v))
	return b
}
