package lint

import (
	"fmt"
	"os"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/feature"
)

// The rounds check. A scenario is one behavior: Given sets up the state,
// When is the one thing someone does, Then is what is true afterwards. A
// scenario that does something, checks it, then does something else and
// checks that (When … Then …, then When … Then … again) is several
// scenarios in one: the first failure hides whether the rest hold, and its
// name can say what only one of them proves.

// RoundsRuleID is the id of the builtin rounds check.
const RoundsRuleID = "one-behavior-per-scenario"

// CheckRounds warns about a scenario of more than one When … Then round, at
// the step that starts its second. Findings are warnings. workDir is what
// reported paths are relative to.
func CheckRounds(pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "One behavior per scenario",
		ID:          RoundsRuleID,
		Description: "A scenario does one thing (When) and checks what is true afterwards (Then); a When after a Then starts another scenario.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, p := range pickles {
		files[p.Doc.Path] = true
		n, second := rounds(p)
		if n < 2 {
			continue
		}
		src := p.StepSource(second)
		l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + second.Text), abs: p.Doc.Path}
		// An outline's examples repeat its steps: report a line once.
		once := fmt.Sprintf("%s:%d", l.abs, l.Line)
		if seen[once] {
			continue
		}
		seen[once] = true
		rr.Findings = append(rr.Findings, Finding{
			Code:     CodeSeveralRounds,
			Severity: SeverityWarning,
			Message: fmt.Sprintf("a When after a Then: this scenario checks %d behaviors in turn (When … Then …, then When … Then … again), which is %d scenarios. "+
				"Write one per behavior: Given the state it starts from (what an earlier round did), When the one thing someone does, Then what is true afterwards", n, n),
			Locations: []Location{l},
		})
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
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
