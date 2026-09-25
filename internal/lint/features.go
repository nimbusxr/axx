package lint

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// SQL ordinals check.
//
// The SQL pack keeps the selections and triggers of a scenario in lists that
// only grow: every retrieval appends, whatever ordinal its step names, and
// an ordinal only addresses an entry when asserting (`the 2nd selection has
// 1 row`). Two mistakes follow, and both can be proven from the feature
// text alone, without knowing which database service is the default:
//
//   - addressing entry N when fewer than N retrievals (of any service)
//     happened earlier in the scenario: the step always fails at runtime
//     (AXX-E0830);
//   - labelling a retrieval "Nth" when it cannot be the Nth entry: when
//     fewer than N-1 retrievals (of any service) came before, or when at
//     least N retrievals from the same service text already came before
//     (AXX-E0831). Later steps that use the label address another entry.
//
// Counting every service gives an upper bound on one service's list and
// counting the same service text a lower bound, so neither warning can be a
// false positive.

// ordinalUse describes how a step uses an ordinal.
type ordinalUse struct {
	list    string // "selection" or "trigger"
	create  bool   // the step appends to the list
	ordinal int    // index of the list ordinal argument
	service int    // index of the {dbService} argument
}

// ordinalSteps maps SQL step ids to their ordinal and service arguments.
// Step text is frozen public API, so the argument positions are stable; a
// test checks them against the registered expressions.
var ordinalSteps = map[string]ordinalUse{
	"sql.select":                    {list: "selection", create: true, ordinal: 0, service: 2},
	"sql.select.poll":               {list: "selection", create: true, ordinal: 1, service: 4},
	"sql.select.jsonb":              {list: "selection", create: true, ordinal: 0, service: 2},
	"sql.json.are":                  {list: "selection", ordinal: 2, service: 3},
	"sql.json.match":                {list: "selection", ordinal: 2, service: 3},
	"sql.rows.eq":                   {list: "selection", ordinal: 0, service: 1},
	"sql.rows.gt":                   {list: "selection", ordinal: 0, service: 1},
	"sql.rows.lt":                   {list: "selection", ordinal: 0, service: 1},
	"sql.trigger.raise":             {list: "trigger", create: true, ordinal: 0, service: 2},
	"sql.trigger.raise.times":       {list: "trigger", create: true, ordinal: 0, service: 2},
	"sql.trigger.insertRaise":       {list: "trigger", create: true, ordinal: 0, service: 2},
	"sql.trigger.insertRaise.times": {list: "trigger", create: true, ordinal: 0, service: 2},
	"sql.trigger.raised":            {list: "trigger", ordinal: 0, service: 2},
}

// OrdinalRuleID is the id of the builtin SQL ordinals check.
const OrdinalRuleID = "sql-ordinals"

// CheckFeatures runs axx's builtin feature-file checks (currently the SQL
// ordinals check) over pickles, matching steps with reg. Findings are
// warnings. workDir is what reported paths are relative to.
func CheckFeatures(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "SQL selection and trigger ordinals",
		ID:          OrdinalRuleID,
		Description: "Selections and triggers are numbered in the order a scenario creates them; the ordinal in a retrieval step is only a label.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, p := range pickles {
		total := map[string]int{}  // list -> retrievals so far (all services)
		byText := map[string]int{} // list + service text -> retrievals so far
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			id := ms[0].Def().Step.ID
			if id == "sql.service" {
				// (Re)registering a service replaces its lists; which list the
				// default service uses may change, so drop the lower bounds.
				byText = map[string]int{}
				continue
			}
			use, ok := ordinalSteps[id]
			if !ok {
				continue
			}
			args := ms[0].Args
			n := 1
			labelled := false
			if use.ordinal < len(args) && args[use.ordinal].Present {
				v, err := parseOrdinal(args[use.ordinal].Raw)
				if err != nil {
					continue
				}
				n, labelled = v, true
			}
			svc := ""
			if use.service < len(args) && args[use.service].Present {
				svc = args[use.service].Raw
			}
			key := use.list + "\x00" + svc
			var f *Finding
			src := p.StepSource(ps)
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: src.Line, Text: clip(strings.TrimSpace(src.Keyword) + " " + ps.Text), abs: p.Doc.Path}
			verb := map[string]string{"selection": "retrieved", "trigger": "created"}[use.list]
			// count renders "only 2 selections were retrieved", "no triggers were created"...
			count := func(n int, only, qualifier string) string {
				if n == 0 {
					return fmt.Sprintf("no %ss%s were %s", use.list, qualifier, verb)
				}
				return fmt.Sprintf("%s%d %s%s%s %s %s", only, n, use.list, plural(n, "", "s"), qualifier, plural(n, "was", "were"), verb)
			}
			labels := fmt.Sprintf("ordinals in these steps are labels only: %ss are numbered in the order they are %s", use.list, verb)
			if use.create {
				switch {
				case labelled && n > total[use.list]+1:
					at := "at most the " + ordinal(total[use.list]+1)
					if total[use.list] == 0 {
						at = "the 1st"
					}
					f = &Finding{Code: CodeOrdinalLabel, Message: fmt.Sprintf(
						"this step is labelled the %s %s, but %s before it in the scenario, so it is %s; %s",
						ordinal(n), use.list, count(total[use.list], "only ", ""), at, labels)}
				case labelled && n <= byText[key]:
					f = &Finding{Code: CodeOrdinalLabel, Message: fmt.Sprintf(
						"this step is labelled the %s %s, but %s before it, so it is at least the %s; %s",
						ordinal(n), use.list, count(byText[key], "", onService(svc)), ordinal(byText[key]+1), labels)}
				}
				total[use.list]++
				byText[key]++
			} else if n > total[use.list] {
				f = &Finding{Code: CodeOrdinalMissing, Message: fmt.Sprintf(
					"this step uses the %s %s, but %s before it in the scenario, so the step always fails",
					ordinal(n), use.list, count(total[use.list], "only ", ""))}
			}
			if f == nil {
				continue
			}
			// Background and outline steps repeat per pickle: report a line once.
			once := fmt.Sprintf("%s:%d:%s", l.abs, l.Line, f.Code)
			if seen[once] {
				continue
			}
			seen[once] = true
			files[p.Doc.Path] = true
			f.Severity = SeverityWarning
			f.Locations = []Location{l}
			rr.Findings = append(rr.Findings, *f)
		}
	}
	for _, p := range pickles {
		files[p.Doc.Path] = true
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
}

func onService(svc string) string {
	if svc == "" {
		return " of the default service"
	}
	return " on " + svc
}

func parseOrdinal(s string) (int, error) {
	s = strings.TrimRight(s, "stndrh")
	return strconv.Atoi(s)
}

// ordinal renders 1 as "1st", 2 as "2nd", 11 as "11th".
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
