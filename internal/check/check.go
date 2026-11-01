// Package check matches the steps of parsed feature files against a step
// registry and reports every step that cannot run: undefined, ambiguous,
// given the wrong argument, or given a value the rule cannot read (ADR 0014). `axx validate`, `axx doctor`, the MCP server and
// the language server all report through it, so they always agree.
package check

import (
	"fmt"
	"strings"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

// Problem kinds.
const (
	Undefined = "undefined"
	Ambiguous = "ambiguous"
	Argument  = "argument"
	// Value is a table cell that could be read two ways (ADR 0014): its
	// Line is the cell's row.
	Value = "value"
)

// ValueCode is the error code of a Value problem.
const ValueCode = "AXX-E0207"

// Problem is one step that cannot run.
type Problem struct {
	Kind string
	// URI and Line locate the step in its feature file (Line is 1-based).
	URI  string
	Line int
	// Text is the step text after Scenario Outline substitution.
	Text string
	// Message explains an argument or value problem.
	Message string
	// Code is the problem's error code, when it has one (ValueCode).
	Code string
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
					for _, vp := range valueProblems(ms[0].Def().Step.Table, ps, p.StepTableLines(ps)) {
						res.Problems = append(res.Problems, Problem{Kind: Value, Code: ValueCode, URI: p.Doc.URI, Line: orLine(vp.line, src.Line), Text: ps.Text, Message: vp.message})
					}
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

type valueProblem struct {
	line    int
	message string
}

// valueProblems are the value cells of a step's table that the rule cannot
// read: an empty cell, a number in a form JSON does not have, a value that
// is not text where only text fits. lines are the rows' lines.
func valueProblems(td *core.TableDoc, ps *messages.PickleStep, lines []int) []valueProblem {
	if td == nil || td.Values == core.NotValues || ps.Argument == nil || ps.Argument.DataTable == nil {
		return nil
	}
	rows := make([][]string, len(ps.Argument.DataTable.Rows))
	for i, r := range ps.Argument.DataTable.Rows {
		for _, c := range r.Cells {
			rows[i] = append(rows[i], c.Value)
		}
	}
	t := td.WithoutNamesRow(&core.Table{Rows: rows})
	skipped := len(rows) - len(t.Rows)
	var out []valueProblem
	for i, row := range t.Rows {
		for _, c := range td.ValueCells(row) {
			cell := strings.TrimSpace(row[c])
			var err error
			switch td.Values {
			case core.JSONValues:
				_, err = core.ParseValue(cell, cell == "", td.Undefined)
			case core.TextValues:
				_, _, err = core.ParseText(cell, cell == "", td.Undefined)
			}
			if err == nil {
				continue
			}
			label := fmt.Sprintf("cell %d", c+1)
			if len(td.Columns) >= 2 {
				label = strings.TrimSpace(row[0])
			}
			line := 0
			if k := skipped + i; k < len(lines) {
				line = lines[k]
			}
			out = append(out, valueProblem{line: line, message: label + ": " + err.Error()})
		}
	}
	return out
}

// orLine is a, or b when a is 0.
func orLine(a, b int) int {
	if a == 0 {
		return b
	}
	return a
}
