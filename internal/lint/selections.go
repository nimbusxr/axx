package lint

import (
	"fmt"
	"os"
	"strings"

	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// The stale selections check. The SQL and MongoDB packs number a scenario's
// selections in the order it retrieves them, and a step that names no
// ordinal (`the selection has 1 row`, `the 1st document for the selection
// …`) checks the first. After a later retrieval, such a step reads as if it
// checked the latest, and when nothing checks the later one, the scenario
// asserts on what came back before: two eval suites checked a stale
// selection that way and missed the bug the later one would have shown.
//
// A finding needs every selection step of the pack in the scenario to name
// the same service (or none), so the count is that service's list.

// selectionUse describes how a step uses a selection ordinal.
type selectionUse struct {
	pack    string
	create  bool // the step retrieves a selection
	ordinal int  // index of the selection ordinal argument
	service int  // index of the service argument
}

// selectionSteps maps the SQL and MongoDB step ids to their selection
// ordinal and service arguments; a test checks them against the registered
// expressions.
var selectionSteps = map[string]selectionUse{
	"sql.select":       {pack: "sql", create: true, ordinal: 0, service: 2},
	"sql.select.poll":  {pack: "sql", create: true, ordinal: 1, service: 4},
	"sql.select.jsonb": {pack: "sql", create: true, ordinal: 0, service: 2},
	"sql.json.are":     {pack: "sql", ordinal: 2, service: 3},
	"sql.json.match":   {pack: "sql", ordinal: 2, service: 3},
	"sql.rows.eq":      {pack: "sql", ordinal: 0, service: 1},
	"sql.rows.gt":      {pack: "sql", ordinal: 0, service: 1},
	"sql.rows.lt":      {pack: "sql", ordinal: 0, service: 1},
	"mongo.find":       {pack: "mongo", create: true, ordinal: 0, service: 2},
	"mongo.find.poll":  {pack: "mongo", create: true, ordinal: 1, service: 4},
	"mongo.docs.eq":    {pack: "mongo", ordinal: 0, service: 1},
	"mongo.docs.gt":    {pack: "mongo", ordinal: 0, service: 1},
	"mongo.docs.lt":    {pack: "mongo", ordinal: 0, service: 1},
	"mongo.doc.are":    {pack: "mongo", ordinal: 1, service: 2},
	"mongo.doc.match":  {pack: "mongo", ordinal: 1, service: 2},
}

// SelectionsRuleID is the id of the builtin stale selections check.
const SelectionsRuleID = "stale-selections"

// CheckSelections warns about a step that checks the first selection while
// a later one, which no step checks, was retrieved before it. Findings are
// warnings. workDir is what reported paths are relative to.
func CheckSelections(reg *match.Registry, pickles []*feature.Pickle, workDir string) RuleResult {
	if workDir == "" {
		workDir, _ = os.Getwd()
	}
	rr := RuleResult{
		Name:        "Stale selections",
		ID:          SelectionsRuleID,
		Description: "A step without an ordinal checks a scenario's first selection, not the latest; a later selection nothing checks is a check that never happens.",
		Type:        "builtin",
		Mode:        ModeWarn,
		Findings:    []Finding{},
	}
	type step struct {
		use  selectionUse
		n    int // the ordinal the step names, 0 for none
		svc  string
		line int
		text string
	}
	seen := map[string]bool{}
	files := map[string]bool{}
	for _, p := range pickles {
		files[p.Doc.Path] = true
		var steps []step
		services := map[string]map[string]bool{} // pack -> service texts
		for _, ps := range p.Steps {
			ms := reg.Match(ps.Text)
			if len(ms) != 1 {
				continue
			}
			use, ok := selectionSteps[ms[0].Def().Step.ID]
			if !ok {
				continue
			}
			args := ms[0].Args
			s := step{use: use}
			if use.ordinal < len(args) && args[use.ordinal].Present {
				v, err := parseOrdinal(args[use.ordinal].Raw)
				if err != nil {
					continue
				}
				s.n = v
			}
			if use.service < len(args) && args[use.service].Present {
				s.svc = args[use.service].Raw
			}
			src := p.StepSource(ps)
			s.line, s.text = src.Line, clip(strings.TrimSpace(src.Keyword)+" "+ps.Text)
			if services[use.pack] == nil {
				services[use.pack] = map[string]bool{}
			}
			services[use.pack][s.svc] = true
			steps = append(steps, s)
		}
		// The selections each step checks, by pack: an ordinal, or the 1st.
		checked := map[string]map[int]bool{}
		for _, s := range steps {
			if s.use.create {
				continue
			}
			if checked[s.use.pack] == nil {
				checked[s.use.pack] = map[int]bool{}
			}
			checked[s.use.pack][max(s.n, 1)] = true
		}
		retrieved := map[string][]int{} // pack -> the line of each selection
		for _, s := range steps {
			if len(services[s.use.pack]) != 1 {
				continue // several services: which list a step uses is not certain
			}
			lines := retrieved[s.use.pack]
			if s.use.create {
				retrieved[s.use.pack] = append(lines, s.line)
				continue
			}
			if s.n != 0 || len(lines) < 2 {
				continue
			}
			// The latest selection before this step that nothing checks.
			stale := 0
			for k := len(lines); k >= 2; k-- {
				if !checked[s.use.pack][k] {
					stale = k
					break
				}
			}
			if stale == 0 {
				continue
			}
			l := Location{File: relSlash(workDir, p.Doc.Path), Line: s.line, Text: s.text, abs: p.Doc.Path}
			once := fmt.Sprintf("%s:%d", l.abs, l.Line)
			if seen[once] {
				continue
			}
			seen[once] = true
			rr.Findings = append(rr.Findings, Finding{
				Code:     CodeStaleSelection,
				Severity: SeverityWarning,
				Message: fmt.Sprintf("this step checks the 1st selection (line %d), not the %s (line %d), which no step checks: a step without an ordinal means the first; to check the %s, name it: `%s`",
					lines[0], ordinal(stale), lines[stale-1], ordinal(stale), withSelectionOrdinal(s.text, stale)),
				Locations: []Location{l},
			})
		}
	}
	for f := range files {
		rr.files = append(rr.files, f)
	}
	rr.Files = len(rr.files)
	sortFindings(rr.Findings)
	return rr
}

// withSelectionOrdinal writes a step as checking the nth selection: "Then
// the selection has 1 row" as "the 2nd selection has 1 row".
func withSelectionOrdinal(text string, n int) string {
	if _, rest, ok := strings.Cut(text, " "); ok {
		text = rest // the keyword
	}
	return strings.Replace(text, "the selection", "the "+ordinal(n)+" selection", 1)
}
