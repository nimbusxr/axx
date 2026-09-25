// Package report implements axx's run reporters: human output (pretty,
// progress), the token-efficient compact and agent formats for coding
// agents, and the interchange formats CI systems understand (JUnit XML,
// Cucumber Messages, legacy Cucumber JSON and the Cucumber HTML report).
package report

import (
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nimbusxr/axx/internal/axxerr"
	"github.com/nimbusxr/axx/internal/exitcode"
	"github.com/nimbusxr/axx/internal/runner"
	"github.com/nimbusxr/axx/internal/version"
)

// CodeUnknownReporter is returned by New for an unsupported reporter name.
const CodeUnknownReporter = "AXX-E0600"

// Options configures a reporter.
type Options struct {
	// Color enables ANSI colors (the caller decides from TTY/NO_COLOR).
	Color bool
	// Version is the axx version recorded in report metadata; defaults to
	// the build version.
	Version string
	// BaseDir makes absolute paths relative in reports.
	BaseDir string
	// RerunCommand returns the command that reruns one scenario, e.g.
	// "axx run features/x.feature:14". Defaults to "axx run <uri>:<line>".
	RerunCommand func(r *runner.ScenarioResult) string
	// Now is an optional clock for deterministic tests.
	Now func() time.Time
}

// reporter names in documentation order.
var names = []string{"pretty", "progress", "compact", "junit", "messages", "cucumber-json", "html", "agent", "teamcity"}

// Names returns the supported reporter names in documentation order.
func Names() []string { return append([]string(nil), names...) }

// NeedsMessages reports whether the named reporter consumes Cucumber
// Messages envelopes, i.e. whether the runner must emit them.
func NeedsMessages(name string) bool {
	return name == "messages" || name == "html" || name == "teamcity"
}

// New creates a reporter writing to w. Unknown names return an *axxerr.Error
// with exit code Usage.
func New(name string, w io.Writer, opts Options) (runner.Reporter, error) {
	if opts.Version == "" {
		opts.Version = version.Get().Version
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.RerunCommand == nil {
		opts.RerunCommand = func(r *runner.ScenarioResult) string { return "axx run " + shellQuote(opts.scenarioLoc(r)) }
	}
	out := &writer{w: w}
	switch name {
	case "pretty":
		return &pretty{h: newHuman(out, opts)}, nil
	case "progress":
		return &progress{h: newHuman(out, opts)}, nil
	case "compact":
		return &compact{out: out, opts: opts}, nil
	case "junit":
		return &junit{out: out, opts: opts}, nil
	case "messages":
		return newMessages(out), nil
	case "cucumber-json":
		return &cucumberJSON{out: out, opts: opts}, nil
	case "html":
		return newHTML(out, embeddedAssets), nil
	case "agent":
		return &agent{out: out, opts: opts}, nil
	case "teamcity":
		return newTeamcity(out, opts), nil
	}
	return nil, axxerr.New(CodeUnknownReporter, exitcode.Usage, "unknown reporter %q", name).
		WithHint("valid reporters: %s", strings.Join(names, ", "))
}

// writer remembers the first write error so reporters can return it from
// Finish (Envelope and ScenarioFinished cannot return errors).
type writer struct {
	w   io.Writer
	err error
}

func (w *writer) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	if err != nil {
		w.err = err
	}
	return n, err
}

// put writes s, recording (not returning) any error.
func (w *writer) put(s string) {
	_, _ = w.Write([]byte(s))
}

// uri returns the scenario's feature file, relative to BaseDir when possible.
func (o *Options) uri(r *runner.ScenarioResult) string {
	if r.Pickle == nil {
		return ""
	}
	u := r.Pickle.Uri
	if o.BaseDir != "" && filepath.IsAbs(u) {
		if rel, err := filepath.Rel(o.BaseDir, u); err == nil && !strings.HasPrefix(rel, "..") {
			u = filepath.ToSlash(rel)
		}
	}
	return u
}

// scenarioLoc is "<uri>:<line>" (the example row line for outline rows).
func (o *Options) scenarioLoc(r *runner.ScenarioResult) string {
	line := 0
	if r.Pickle != nil {
		line = r.Pickle.Line
	}
	return loc(o.uri(r), line)
}

func (o *Options) stepLoc(r *runner.ScenarioResult, s *runner.StepResult) string {
	if s.Hook != nil || s.Line == 0 {
		return ""
	}
	return loc(o.uri(r), s.Line)
}

func (o *Options) rerun(r *runner.ScenarioResult) string {
	if o.RerunCommand == nil {
		return ""
	}
	return o.RerunCommand(r)
}

// shellQuote quotes s for POSIX shells when it contains special characters.
func shellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, unsafeShellRune) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func unsafeShellRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	}
	return !strings.ContainsRune("_-./:@%+=,", r)
}

func loc(uri string, line int) string {
	if line <= 0 {
		return uri
	}
	return uri + ":" + strconv.Itoa(line)
}

func scenarioName(r *runner.ScenarioResult) string {
	if r.Pickle == nil {
		return ""
	}
	return r.Pickle.Name
}

func featureName(r *runner.ScenarioResult) string {
	if r.Pickle == nil || r.Pickle.Doc == nil || r.Pickle.Doc.AST == nil || r.Pickle.Doc.AST.Feature == nil {
		return ""
	}
	return r.Pickle.Doc.AST.Feature.Name
}

func scenarioKeyword(r *runner.ScenarioResult) string {
	if r.Pickle != nil && r.Pickle.Keyword != "" {
		return r.Pickle.Keyword
	}
	return "Scenario"
}

// isOutlineRow reports whether the scenario is an example row of a
// Scenario Outline.
func isOutlineRow(r *runner.ScenarioResult) bool {
	return r.Pickle != nil && r.Pickle.Pickle != nil && len(r.Pickle.AstNodeIds) > 1
}

// failing reports whether a scenario status makes the run unsuccessful.
func failing(s runner.Status) bool { return s >= runner.Pending }

// culprit returns the step or hook responsible for the scenario's status:
// the first one whose status equals the scenario's, else the first failure.
func culprit(r *runner.ScenarioResult) *runner.StepResult {
	for _, group := range [][]*runner.StepResult{r.Before, r.Steps, r.After} {
		for _, s := range group {
			if s.Status == r.Status {
				return s
			}
		}
	}
	return r.FirstFailure()
}

// phase says where a result sits in a scenario.
type phase int

const (
	phaseBefore phase = iota // Before hook
	phaseStep                // Gherkin step
	phaseAfter               // After hook or scenario cleanup
)

// stepTitle is "Keyword text" for steps and a descriptive label for hooks.
func stepTitle(s *runner.StepResult, p phase) string {
	switch p {
	case phaseBefore:
		if s.Hook != nil {
			return "Before hook " + s.Hook.ID
		}
		return "Before " + s.Text
	case phaseAfter:
		if s.Hook != nil {
			return "After hook " + s.Hook.ID
		}
		return s.Text
	}
	if s.Keyword == "" {
		return s.Text
	}
	return s.Keyword + " " + s.Text
}

// allSteps visits hooks and steps in execution order.
func allSteps(r *runner.ScenarioResult, fn func(s *runner.StepResult, p phase)) {
	for _, s := range r.Before {
		fn(s, phaseBefore)
	}
	for _, s := range r.Steps {
		fn(s, phaseStep)
	}
	for _, s := range r.After {
		fn(s, phaseAfter)
	}
}

// phaseOf locates s in r.
func phaseOf(r *runner.ScenarioResult, s *runner.StepResult) phase {
	for _, b := range r.Before {
		if b == s {
			return phaseBefore
		}
	}
	for _, a := range r.After {
		if a == s {
			return phaseAfter
		}
	}
	return phaseStep
}

// fmtDuration renders a duration compactly: 250ms, 12.4s, 2m03.5s.
func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	default:
		m := int64(d / time.Minute)
		s := (d % time.Minute).Seconds()
		sec := strconv.FormatFloat(s, 'f', 1, 64)
		if s < 10 {
			sec = "0" + sec
		}
		return strconv.FormatInt(m, 10) + "m" + sec + "s"
	}
}

// statusOrder lists statuses worst-first, the order summaries use.
var statusOrder = []runner.Status{runner.Failed, runner.Ambiguous, runner.Undefined, runner.Pending, runner.Skipped, runner.Passed}
