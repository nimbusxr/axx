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
// to judge: a success status code says a request was accepted, not what it
// did, and a check that something did not happen also passes when the action
// never ran. Like every hint, they need no fix and never change the exit
// code.

// scenarioHintMax is how many scenarios one hint names.
const scenarioHintMax = 5

// ScenarioHints names the scenarios whose checks (the steps defined as
// Then) are only success status codes of REST responses, and those whose
// checks are only absences (steps defined with Absence). A scenario with a
// step that matches no single definition is left out.
func ScenarioHints(reg *match.Registry, pickles []*feature.Pickle, opts Options) []string {
	if opts.WorkDir == "" {
		opts.WorkDir, _ = os.Getwd()
	}
	within := withinPaths(opts)
	var statusOnly, absenceOnly []string
	seen := map[string]bool{}
	for _, p := range pickles {
		loc := fmt.Sprintf("%s:%d", relSlash(opts.WorkDir, p.Doc.Path), p.ScenarioLine)
		if seen[loc] || (len(opts.Paths) > 0 && !within(p.Doc.Path)) {
			continue // an outline is one scenario, whichever of its examples
		}
		checks, successes, absences := 0, 0, 0
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
			switch {
			case def.Step.Absence:
				absences++
			case def.Step.ID == "rest.response.status" && success(ms[0].Args):
				successes++
			}
		}
		if !matched || checks == 0 {
			continue
		}
		switch checks {
		case successes:
			statusOnly = append(statusOnly, loc)
		case absences:
			absenceOnly = append(absenceOnly, loc)
		default:
			continue
		}
		seen[loc] = true
	}
	var hints []string
	if n := len(statusOnly); n > 0 {
		hints = append(hints, fmt.Sprintf("%d %s only a success status code, which says a request was accepted, not what it did: %s (check what it did too: a response property, a row, a message)",
			n, plural(n, "scenario checks", "scenarios check"), scenarioList(statusOnly)))
	}
	if n := len(absenceOnly); n > 0 {
		hints = append(hints, fmt.Sprintf("%d %s only that something did not happen, which also passes when the action never ran: %s (check something it did do too)",
			n, plural(n, "scenario checks", "scenarios check"), scenarioList(absenceOnly)))
	}
	return hints
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
