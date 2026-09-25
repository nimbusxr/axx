package report

import (
	"strconv"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/runner"
)

// Limits for step logs in compact output.
const (
	compactMaxLogs    = 5
	compactMaxLogRune = 200
)

// compact is the token-efficient format for coding agents: only problems,
// no color or decoration, then a one-line summary.
type compact struct {
	out  *writer
	opts Options
	// seen dedupes undefined/ambiguous steps shared by several scenarios
	// (e.g. Background steps).
	seen map[string]bool
}

func (c *compact) Envelope(*messages.Envelope) {}

func (c *compact) ScenarioFinished(r *runner.ScenarioResult) {
	if !failing(r.Status) {
		return
	}
	var b strings.Builder
	switch r.Status {
	case runner.Failed:
		c.problem(&b, "FAIL", r)
		if cmd := c.opts.rerun(r); cmd != "" {
			b.WriteString("  rerun " + cmd + "\n")
		}
	case runner.Pending:
		c.problem(&b, "PENDING", r)
	}
	allSteps(r, func(s *runner.StepResult, p phase) {
		switch s.Status {
		case runner.Undefined:
			c.stepProblem(&b, "UNDEFINED", r, s, p)
		case runner.Ambiguous:
			c.stepProblem(&b, "AMBIGUOUS", r, s, p)
		}
	})
	c.out.put(b.String())
}

// problem reports a scenario-level failure with its culprit step.
func (c *compact) problem(b *strings.Builder, label string, r *runner.ScenarioResult) {
	b.WriteString(label + " " + c.opts.scenarioLoc(r) + "  " + oneLine(scenarioName(r)) + "\n")
	s := culprit(r)
	if s == nil {
		return
	}
	title := oneLine(stepTitle(s, phaseOf(r, s)))
	if l := c.opts.stepLoc(r, s); l != "" {
		b.WriteString("  step " + l + "  " + title + "\n")
	} else {
		b.WriteString("  " + lowerFirst(title) + "\n")
	}
	if f := describe(s); f != nil && (s.Status == runner.Failed || f.message != "pending") {
		writeIndented(b, "  ", f.compactLines(c.opts.BaseDir))
	}
	logs := s.Logs
	if len(logs) > compactMaxLogs {
		logs = logs[len(logs)-compactMaxLogs:]
	}
	for _, l := range logs {
		b.WriteString("  log " + truncateRunes(oneLine(stripANSI(l)), compactMaxLogRune) + "\n")
	}
}

// stepProblem reports an undefined or ambiguous step once per location.
func (c *compact) stepProblem(b *strings.Builder, label string, r *runner.ScenarioResult, s *runner.StepResult, p phase) {
	where := c.opts.stepLoc(r, s)
	if where == "" {
		where = c.opts.scenarioLoc(r)
	}
	key := label + " " + where + " " + s.Text
	if c.seen == nil {
		c.seen = map[string]bool{}
	}
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	b.WriteString(label + " " + where + "  " + oneLine(stepTitle(s, p)) + "\n")
	if f := describe(s); f != nil {
		writeIndented(b, "  ", f.compactLines(c.opts.BaseDir))
	}
}

func (c *compact) Finish(res *runner.RunResult) error {
	for _, re := range res.RunErrors {
		var b strings.Builder
		b.WriteString("FAIL run  " + re.Source + "\n")
		for _, l := range strings.Split(strings.TrimRight(re.Message, "\n"), "\n") {
			b.WriteString("  " + l + "\n")
		}
		c.out.put(b.String())
	}
	counts := res.Counts().Scenarios
	parts := []string{"PASS " + strconv.Itoa(counts[runner.Passed])}
	for _, st := range []runner.Status{runner.Failed, runner.Undefined, runner.Ambiguous, runner.Pending, runner.Skipped} {
		if n := counts[st]; n > 0 {
			parts = append(parts, compactLabel(st)+" "+strconv.Itoa(n))
		}
	}
	if n := len(res.RunErrors); n > 0 {
		parts = append(parts, "RUN FAIL "+strconv.Itoa(n))
	}
	if res.NotRun > 0 {
		parts = append(parts, "NOT RUN "+strconv.Itoa(res.NotRun))
	}
	if res.Interrupted {
		parts = append(parts, "INTERRUPTED")
	}
	if res.DryRun {
		parts = append(parts, "DRY RUN")
	}
	parts = append(parts, fmtDuration(res.Duration))
	c.out.put(strings.Join(parts, "  ") + "\n")
	return c.out.err
}

func compactLabel(st runner.Status) string {
	switch st {
	case runner.Failed:
		return "FAIL"
	case runner.Skipped:
		return "SKIP"
	case runner.Passed:
		return "PASS"
	}
	return strings.ToUpper(st.String())
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// oneLine flattens newlines so every compact record stays line-oriented.
func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", `\n`)
}
