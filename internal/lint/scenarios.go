package lint

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Scenario hints name scenarios whose checks prove little, for their author
// to judge: a success status (a 2xx response, a command's exit code 0) says
// something was accepted, not what it did, and a check that something did not happen also passes when the action
// never ran. Like every hint, they need no fix and never change the exit
// code.

// scenarioHintMax is how many scenarios one hint names.
const scenarioHintMax = 5

// ScenarioHints names the scenarios whose checks (the steps defined as
// Then) are only success statuses (a REST response's 2xx, a command's exit
// code 0), only REST status codes with a refusal among them, or only
// absences (steps defined with Absence). A scenario with a step that matches
// no single definition is left out.
func ScenarioHints(reg *match.Registry, pickles []*feature.Pickle, opts Options) []string {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	within := withinPaths(opts)
	var statusOnly, refusalOnly, absenceOnly []string
	seen := map[string]bool{}
	for _, p := range pickles {
		loc := fmt.Sprintf("%s:%d", relSlash(opts.WorkDir, p.Doc.Path), p.ScenarioLine)
		if len(opts.Paths) > 0 && !within(p.Doc.Path) {
			continue
		}
		if seen[loc] {
			continue // an outline is one scenario, whichever of its examples
		}
		checks, successes, statuses, absences := 0, 0, 0, 0
		matched := true
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				matched = false
				break
			}
			def := ms[0].Def()
			if def.Step.Keyword != "Then" {
				continue
			}
			checks++
			if def.Step.ID == "rest.response.status" {
				statuses++
			}
			switch {
			case def.Step.Absence:
				absences++
			case def.Step.ID == "rest.response.status" && success(ms[0].Args),
				def.Step.ID == "cli.exit" && exitedZero(ms[0].Args):
				successes++
			}
		}
		if !matched || checks == 0 {
			continue
		}
		switch checks {
		case successes:
			statusOnly = append(statusOnly, loc)
		case statuses:
			// Status codes only, a refusal among them: a request refused
			// for another reason than the one meant passes as well.
			refusalOnly = append(refusalOnly, loc)
		case absences:
			absenceOnly = append(absenceOnly, loc)
		default:
			continue
		}
		seen[loc] = true
	}
	var hints []string
	if n := len(statusOnly); n > 0 {
		hints = append(hints, fmt.Sprintf("%d %s only a success status (a 2xx response, a command's exit code 0), which says it was accepted, not what it did: %s (check what it did too: a response property, a row, a message, the output)",
			n, plural(n, "scenario checks", "scenarios check"), scenarioList(statusOnly)))
	}
	if n := len(refusalOnly); n > 0 {
		hints = append(hints, fmt.Sprintf("%d %s only status codes, a refusal among them: a request refused for another reason (a missing field) passes as well as one refused by the rule meant: %s (check what the response says too, like the problem detail naming the field)",
			n, plural(n, "scenario checks", "scenarios check"), scenarioList(refusalOnly)))
	}
	if n := len(absenceOnly); n > 0 {
		hints = append(hints, fmt.Sprintf("%d %s only that something did not happen, which also passes when the action never ran: %s (check something it did do too)",
			n, plural(n, "scenario checks", "scenarios check"), scenarioList(absenceOnly)))
	}
	return hints
}

// exitedZero reports whether an exit code step's code is 0.
func exitedZero(args []core.Arg) bool {
	for _, a := range args {
		if a.Present && a.Param == "int" {
			return a.Raw == "0"
		}
	}
	return false
}

// success reports whether a status code step's code is 2xx.
func success(args []core.Arg) bool {
	for _, a := range args {
		if a.Present && a.Param == "int" {
			code, err := strconv.Atoi(a.Raw)
			return err == nil && code >= 200 && code < 300
		}
	}
	return false
}

// scenarioList lists the first scenarioHintMax scenarios.
func scenarioList(locs []string) string {
	if len(locs) <= scenarioHintMax {
		return strings.Join(locs, ", ")
	}
	return strings.Join(locs[:scenarioHintMax], ", ") + fmt.Sprintf(" and %d more", len(locs)-scenarioHintMax)
}
