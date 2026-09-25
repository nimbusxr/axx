package report

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// teamcity reports the run as TeamCity service messages in the ID-based
// tree form that IntelliJ-based IDEs read (and the axx VS Code extension):
// a suite per feature, a suite per scenario (per example row of an
// outline), and a test per step, streamed as they run. Scenarios run in
// parallel, so their messages interleave; node ids keep them apart.
type teamcity struct {
	out  *writer
	opts Options
	h    *human // for the closing summary

	nodes     map[string]astNode // Gherkin AST id -> node
	docs      map[string]string  // uri -> feature node (for features seen)
	features  map[string]astNode // uri -> feature
	pickles   map[string]*messages.Pickle
	cases     map[string]*messages.TestCase // test case id
	started   map[string]string             // test case started id -> test case id
	hooks     map[string]string             // hook id -> name
	results   map[string]*runner.StepResult // started id + "/" + step id
	openFeats []string                      // feature nodes, in start order
	begun     bool
}

// astNode is what the reporter needs of a Gherkin element.
type astNode struct {
	uri     string
	line    int
	keyword string
	name    string
	row     int // 1-based position of an Examples row within its outline
}

func newTeamcity(out *writer, opts Options) *teamcity {
	return &teamcity{
		out: out, opts: opts, h: newHuman(out, Options{BaseDir: opts.BaseDir, RerunCommand: opts.RerunCommand}),
		nodes: map[string]astNode{}, docs: map[string]string{}, features: map[string]astNode{},
		pickles: map[string]*messages.Pickle{}, cases: map[string]*messages.TestCase{}, started: map[string]string{},
		hooks: map[string]string{}, results: map[string]*runner.StepResult{},
	}
}

func (t *teamcity) msg(name string, attrs ...string) {
	var b strings.Builder
	b.WriteString("##teamcity[" + name)
	for i := 0; i+1 < len(attrs); i += 2 {
		b.WriteString(" " + attrs[i] + "='" + tcEscape(attrs[i+1]) + "'")
	}
	b.WriteString("]\n")
	t.out.put(b.String())
}

func (t *teamcity) begin() {
	if !t.begun {
		t.begun = true
		t.msg("enteredTheMatrix")
		t.msg("testingStarted")
	}
}

// location is a file:// URI with a line, as IntelliJ's locators read it.
func (t *teamcity) location(uri string, line int) string {
	// Feature URIs are relative to the project directory.
	p := uri
	if !filepath.IsAbs(p) {
		if t.opts.BaseDir != "" {
			p = filepath.Join(t.opts.BaseDir, p)
		} else if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
	}
	p = filepath.ToSlash(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths
	}
	return "file://" + p + ":" + strconv.Itoa(line)
}

func (t *teamcity) Envelope(e *messages.Envelope) {
	switch {
	case e.GherkinDocument != nil:
		t.index(e.GherkinDocument)
	case e.Pickle != nil:
		t.pickles[e.Pickle.Id] = e.Pickle
	case e.Hook != nil:
		t.hooks[e.Hook.Id] = e.Hook.Name
	case e.TestCase != nil:
		t.cases[e.TestCase.Id] = e.TestCase
	case e.TestCaseStarted != nil:
		t.begin()
		t.caseStarted(e.TestCaseStarted)
	case e.TestStepStarted != nil:
		t.stepStarted(e.TestStepStarted)
	case e.TestStepFinished != nil:
		t.stepFinished(e.TestStepFinished)
	case e.TestCaseFinished != nil:
		t.msg("testSuiteFinished", "nodeId", e.TestCaseFinished.TestCaseStartedId)
	}
}

// StepFinished receives a step's full result just before its
// TestStepFinished envelope, for the expected and actual values and the
// step's logs and attachments.
func (t *teamcity) StepFinished(caseStartedID, stepID string, sr *runner.StepResult) {
	t.results[caseStartedID+"/"+stepID] = sr
}

func (t *teamcity) ScenarioFinished(*runner.ScenarioResult) {}

func (t *teamcity) Finish(res *runner.RunResult) error {
	t.begin()
	for _, f := range t.openFeats {
		t.msg("testSuiteFinished", "nodeId", f)
	}
	t.msg("testingFinished")
	t.out.put("\n" + t.h.summary(res))
	return t.out.err
}

func (t *teamcity) index(doc *messages.GherkinDocument) {
	f := doc.Feature
	if f == nil {
		return
	}
	t.features[doc.Uri] = astNode{uri: doc.Uri, line: int(f.Location.Line), keyword: f.Keyword, name: f.Name}
	var walk func(children []*messages.FeatureChild, rules []*messages.RuleChild)
	steps := func(ss []*messages.Step) {
		for _, s := range ss {
			t.nodes[s.Id] = astNode{uri: doc.Uri, line: int(s.Location.Line), keyword: s.Keyword, name: s.Text}
		}
	}
	scenario := func(sc *messages.Scenario) {
		t.nodes[sc.Id] = astNode{uri: doc.Uri, line: int(sc.Location.Line), keyword: sc.Keyword, name: sc.Name}
		steps(sc.Steps)
		n := 0
		for _, ex := range sc.Examples {
			for _, row := range ex.TableBody {
				n++
				t.nodes[row.Id] = astNode{uri: doc.Uri, line: int(row.Location.Line), row: n}
			}
		}
	}
	walk = func(children []*messages.FeatureChild, rules []*messages.RuleChild) {
		for _, c := range children {
			switch {
			case c.Background != nil:
				steps(c.Background.Steps)
			case c.Scenario != nil:
				scenario(c.Scenario)
			case c.Rule != nil:
				walk(nil, c.Rule.Children)
			}
		}
		for _, c := range rules {
			switch {
			case c.Background != nil:
				steps(c.Background.Steps)
			case c.Scenario != nil:
				scenario(c.Scenario)
			}
		}
	}
	walk(f.Children, nil)
}

func (t *teamcity) caseStarted(tcs *messages.TestCaseStarted) {
	t.started[tcs.Id] = tcs.TestCaseId
	tc := t.cases[tcs.TestCaseId]
	if tc == nil {
		return
	}
	p := t.pickles[tc.PickleId]
	if p == nil {
		return
	}
	feat, ok := t.docs[p.Uri]
	if !ok {
		feat = "feature-" + strconv.Itoa(len(t.docs)+1)
		t.docs[p.Uri] = feat
		f := t.features[p.Uri]
		t.msg("testSuiteStarted", "nodeId", feat, "parentNodeId", "0",
			"name", strings.TrimSpace(f.keyword+": "+f.name), "locationHint", t.location(p.Uri, max(f.line, 1)))
		t.openFeats = append(t.openFeats, feat)
	}
	// An outline example is located at its Examples row.
	sc, line, row := astNode{}, 0, 0
	for _, id := range p.AstNodeIds {
		n, ok := t.nodes[id]
		if !ok {
			continue
		}
		if n.row > 0 {
			line, row = n.line, n.row
		} else {
			sc = n
			if line == 0 {
				line = n.line
			}
		}
	}
	name := strings.TrimSpace(sc.keyword + ": " + p.Name)
	if row > 0 {
		name += " (example " + strconv.Itoa(row) + ")"
	}
	t.msg("testSuiteStarted", "nodeId", tcs.Id, "parentNodeId", feat, "name", name, "locationHint", t.location(p.Uri, max(line, 1)))
}

// step returns the display name and line of a test step, and whether it is
// a hook.
func (t *teamcity) step(caseStartedID, stepID string) (name string, uri string, line int, hook bool) {
	tc := t.cases[t.started[caseStartedID]]
	if tc == nil {
		return stepID, "", 0, false
	}
	p := t.pickles[tc.PickleId]
	before := true
	for _, ts := range tc.TestSteps {
		if ts.PickleStepId != "" && ts.Id != stepID {
			before = false
		}
		if ts.Id != stepID {
			continue
		}
		if ts.HookId != "" {
			kind := "After hook"
			if before {
				kind = "Before hook"
			}
			return kind + " " + t.hooks[ts.HookId], p.Uri, 0, true
		}
		if p != nil {
			for _, ps := range p.Steps {
				if ps.Id != ts.PickleStepId {
					continue
				}
				for _, id := range ps.AstNodeIds {
					if n, ok := t.nodes[id]; ok {
						return n.keyword + ps.Text, p.Uri, n.line, false
					}
				}
				return ps.Text, p.Uri, 0, false
			}
		}
	}
	return stepID, "", 0, false
}

func (t *teamcity) stepStarted(s *messages.TestStepStarted) {
	name, uri, line, hook := t.step(s.TestCaseStartedId, s.TestStepId)
	if hook {
		return // shown only when it fails
	}
	attrs := []string{"nodeId", s.TestCaseStartedId + "/" + s.TestStepId, "parentNodeId", s.TestCaseStartedId, "name", name}
	if uri != "" && line > 0 {
		attrs = append(attrs, "locationHint", t.location(uri, line))
	}
	t.msg("testStarted", attrs...)
}

func (t *teamcity) stepFinished(s *messages.TestStepFinished) {
	id := s.TestCaseStartedId + "/" + s.TestStepId
	sr := t.results[id]
	delete(t.results, id)
	name, _, _, hook := t.step(s.TestCaseStartedId, s.TestStepId)
	status := runner.Passed
	if sr != nil {
		status = sr.Status
	} else if s.TestStepResult != nil && s.TestStepResult.Status != messages.TestStepResultStatus_PASSED {
		status = runner.Failed
	}
	if hook {
		if status == runner.Passed || status == runner.Skipped {
			return
		}
		t.msg("testStarted", "nodeId", id, "parentNodeId", s.TestCaseStartedId, "name", name)
	}
	if sr != nil {
		if out := stepOutput(sr); out != "" {
			t.msg("testStdOut", "nodeId", id, "out", out)
		}
	}
	switch status {
	case runner.Passed:
	case runner.Skipped:
		t.msg("testIgnored", "nodeId", id, "message", "skipped")
	case runner.Pending:
		msg := "pending"
		if sr != nil && sr.Err != nil {
			msg = sr.Err.Error()
		}
		t.msg("testIgnored", "nodeId", id, "message", msg)
	default:
		t.failed(id, sr, s.TestStepResult)
	}
	ms := int64(0)
	if sr != nil {
		ms = sr.Duration.Milliseconds()
	} else if s.TestStepResult != nil && s.TestStepResult.Duration != nil {
		ms = messages.DurationToGoDuration(*s.TestStepResult.Duration).Milliseconds()
	}
	t.msg("testFinished", "nodeId", id, "duration", strconv.FormatInt(ms, 10))
}

func (t *teamcity) failed(id string, sr *runner.StepResult, res *messages.TestStepResult) {
	if sr == nil {
		msg := "failed"
		if res != nil && res.Message != "" {
			msg = res.Message
		}
		t.msg("testFailed", "nodeId", id, "message", firstLine(msg), "details", msg)
		return
	}
	f := describe(sr)
	if f == nil {
		f = &failure{kind: KindError, message: "failed"}
	}
	message, details := f.headline(), strings.Join(f.lines(style{}), "\n")
	if f.kind == KindUndefined {
		message = "Undefined step: no step matches this text"
		details = strings.TrimSpace(details + "\nsearch the steps with `axx steps search <words>`")
	}
	if details == "" {
		details = message
	}
	attrs := []string{"nodeId", id, "message", message, "details", details}
	if f.values {
		attrs = append(attrs, "type", "comparisonFailure", "expected", renderValue(f.expected, 1<<16), "actual", renderValue(f.actual, 1<<16))
	}
	t.msg("testFailed", attrs...)
}

// stepOutput is a step's logs and attachments, as the console shows them.
func stepOutput(sr *runner.StepResult) string {
	var b strings.Builder
	for _, l := range sr.Logs {
		b.WriteString("log: " + l + "\n")
	}
	for _, a := range sr.Attachments {
		fmt.Fprintf(&b, "attachment: %s (%s, %d B)\n", attachmentName(a), a.MediaType, len(a.Body))
		if utf8.Valid(a.Body) && !strings.HasPrefix(a.MediaType, "image/") {
			body := a.Body
			const limit = 4096
			if len(body) > limit {
				b.Write(body[:limit])
				fmt.Fprintf(&b, "\n… (%d more bytes)\n", len(body)-limit)
			} else {
				b.Write(body)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func attachmentName(a runner.Attachment) string {
	if a.Name != "" {
		return a.Name
	}
	return "attachment"
}

// tcEscape escapes a TeamCity service message value.
func tcEscape(s string) string {
	return strings.NewReplacer(
		"|", "||", "'", "|'", "\n", "|n", "\r", "|r", "[", "|[", "]", "|]",
		"\u0085", "|x", " ", "|l", " ", "|p",
	).Replace(s)
}
