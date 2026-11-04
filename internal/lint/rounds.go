package lint

import (
	"fmt"
	"os"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/feature"
)

// The rounds check. A scenario is one behavior: Given sets up the state,
// When is the one thing someone does, Then is what is true afterwards, and And
// adds to whichever it follows. A scenario that does something, checks it, then
// does something else and checks that (When … Then …, then When … Then … again)
// is several scenarios in one: the first failure hides whether the rest hold, and
// its name can say what only one of them proves. A second When or Then without
// that is the same thing begun, or an And written another way.

// RoundsRuleID is the id of the builtin rounds check.
const RoundsRuleID = "one-behavior-per-scenario"

// CheckRounds reports a scenario of more than one When … Then round, at the step
// that starts its second, and otherwise each second When and second Then. Findings
// are errors, unlike the other builtin checks': `axx lint` fails on them unless
// lint.config.mode or --mode says warn. workDir is what reported paths are
// relative to.
func CheckRounds(pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "One behavior per scenario",
		ID:          RoundsRuleID,
		Description: "A scenario does one thing (When) and checks what is true afterwards (Then), with And for more of either; a second When or Then starts another scenario.",
		Type:        "builtin",
		Mode:        ModeError,
		Findings:    []Finding{},
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	report := func(p *feature.Pickle, ps *messages.PickleStep, code, message string) {
		src := p.StepSource(ps)
		l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
		// An outline's examples repeat its steps: report a line once.
		once := fmt.Sprintf("%s:%d", l.abs, l.Line)
		if seen[once] {
			return
		}
		seen[once] = true
		rr.Findings = append(rr.Findings, Finding{Code: code, Severity: SeverityError, Message: message, Locations: []Location{l}})
	}
	for _, p := range pickles {
		files[p.Doc.Path] = true
		if n, second := rounds(p); n >= 2 {
			report(p, second, CodeSeveralRounds, fmt.Sprintf("a When after a Then: this scenario checks %d behaviors in turn (When … Then …, then When … Then … again), which is %d scenarios. "+
				"Write one per behavior: Given the state it starts from (what an earlier round did), When the one thing someone does, Then what is true afterwards", n, n))
			continue
		}
		for _, c := range secondClauses(p) {
			report(p, c.step, CodeSecondClause, c.message)
		}
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
}

type secondClause struct {
	step    *messages.PickleStep
	message string
}

// secondClauses are the steps of a scenario that say When or Then a second time.
func secondClauses(p *feature.Pickle) []secondClause {
	var found []secondClause
	whens, thens := 0, 0
	for _, ps := range p.Steps {
		switch p.StepSource(ps).KeywordType {
		case messages.StepKeywordType_ACTION:
			if whens++; whens > 1 {
				found = append(found, secondClause{ps, "a second When: a scenario is one behavior, so it says When once. " +
					"Write And if this is part of the one thing someone does, and a scenario of its own if it is another"})
			}
		case messages.StepKeywordType_OUTCOME:
			if thens++; thens > 1 {
				found = append(found, secondClause{ps, "a second Then: a scenario is one behavior, so it says Then once. " +
					"Write And if this is also true afterwards, and a scenario of its own if it is another behavior"})
			}
		}
	}
	return found
}

// rounds counts a scenario's When … Then rounds: actions followed by checks
// (And and But take the kind of the step before them). Checks before any
// action, of the state a scenario starts from, are no round of their own.
// second is the step that starts the second round, if there is one.
func rounds(p *feature.Pickle) (n int, second *messages.PickleStep) {
	acted := false
	var start *messages.PickleStep
	for _, ps := range p.Steps {
		switch ps.Type {
		case messages.PickleStepType_ACTION:
			if !acted {
				start = ps
			}
			acted = true
		case messages.PickleStepType_OUTCOME:
			if acted {
				n++
				if n == 2 {
					second = start
				}
				acted = false
			}
		}
	}
	return n, second
}
