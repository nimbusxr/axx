// Package check matches the steps of parsed feature files against a step
// registry and reports every step that cannot run: undefined, ambiguous, or
// given the wrong argument. `axx validate`, `axx doctor`, the MCP server and
// the language server all report through it, so they always agree.
package check

import (
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Problem kinds.
const (
	Undefined = "undefined"
	Ambiguous = "ambiguous"
	Argument  = "argument"
)

// Problem is one step that cannot run.
type Problem struct {
	Kind string
	// URI and Line locate the step in its feature file (Line is 1-based).
	URI  string
	Line int
	// Text is the step text after Scenario Outline substitution.
	Text string
	// Message explains an argument problem.
	Message string
	// Suggestions are the closest steps to an undefined one.
	Suggestions []match.Suggestion
	// Candidates are the matching definitions of an ambiguous step, as
	// "id: expression".
	Candidates []string
}

// Result is the outcome of checking pickles.
type Result struct {
	// Steps counts every pickle step, including repeats of Background and
	// Scenario Outline steps.
	Steps    int
	Problems []Problem
}

// Pickles checks every step of pickles. A step shared by several pickles (a
// Background step, or an outline step with the same text) is reported once.
func Pickles(reg *match.Registry, pickles []*feature.Pickle) Result {
	res := Result{Problems: []Problem{}}
	type key struct {
		uri  string
		line int
		text string
	}
	seen := map[key]bool{}
	for _, p := range pickles {
		for _, ps := range p.Steps {
			res.Steps++
			src := p.StepSource(ps)
			k := key{p.Doc.URI, src.Line, ps.Text}
			if seen[k] {
				continue
			}
			seen[k] = true
			pr := Problem{URI: p.Doc.URI, Line: src.Line, Text: ps.Text}
			ms := reg.Match(ps.Text)
			switch len(ms) {
			case 0:
				pr.Kind = Undefined
				pr.Suggestions = reg.Suggest(ps.Text, 3)
			case 1:
				hasTable := ps.Argument != nil && ps.Argument.DataTable != nil
				hasDoc := ps.Argument != nil && ps.Argument.DocString != nil
				msg := ArgumentProblem(ms[0], hasTable, hasDoc)
				if msg == "" {
					continue
				}
				pr.Kind, pr.Message = Argument, msg
			default:
				pr.Kind = Ambiguous
				for _, m := range ms {
					pr.Candidates = append(pr.Candidates, m.Def().Step.ID+": "+m.Variant.Expr)
				}
			}
			res.Problems = append(res.Problems, pr)
		}
	}
	return res
}

// ArgumentProblem says what is wrong with the data table or doc string given
// to a matched step, or returns "" when it is right.
func ArgumentProblem(m match.Match, hasTable, hasDoc bool) string {
	switch m.Def().Step.Arg.String() {
	case "table":
		if !hasTable {
			return "this step requires a data table"
		}
	case "docstring":
		if !hasDoc {
			return "this step requires a doc string"
		}
	case "none":
		if hasTable {
			return "this step does not accept a data table"
		}
		if hasDoc {
			return "this step does not accept a doc string"
		}
	}
	return ""
}
