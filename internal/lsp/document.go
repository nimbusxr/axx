package lsp

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	gherkin "github.com/cucumber/gherkin/go/v42"
	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/check"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/lint"
	"github.com/nimbusxr/axx/internal/match"
)

// stepLine is a step as it is written in a feature file.
type stepLine struct {
	line      int    // zero-based
	keyword   string // as written, without trailing space: "Given", "And", "*"
	textStart int    // byte offset of the step text within the line
	text      string // the step text as written (with <placeholders> in an outline)
	outline   bool   // the step belongs to a Scenario Outline
	block     int    // the Background or Scenario (Outline) it belongs to
}

// table is a data table under a step, or an Examples table, as it is
// written.
type table struct {
	step  int // the line of the step it belongs to; -1 for Examples
	block int // the Background or Scenario (Outline) it belongs to
	rows  []row
}

// row is a table row: its cells, in order.
type row struct {
	line  int
	cells []cell
}

// cell is a table cell as it is written.
type cell struct {
	text     string // trimmed
	from, to int    // byte offsets of text in the line
	// inner is the byte range between the cell's pipes, whitespace included.
	innerFrom, innerTo int
	// open marks a last cell with no closing pipe yet, while it is typed;
	// Gherkin leaves it out.
	open bool
	// escaped marks a cell with an escape (\|, \n, \\): its value is not
	// its text.
	escaped bool
}

// document is an open feature file and what the server knows about it.
type document struct {
	uri     string
	version int
	lines   []string
	steps   []stepLine
	tables  []table
	// expansions are the step texts an outline step takes, one per example
	// row, keyed by line.
	expansions map[int][]string
	pickles    []*feature.Pickle
	parseErr   error
}

func newDocument(uri string, version int, text string) *document {
	d := &document{uri: uri, version: version, lines: splitLines(text), expansions: map[int][]string{}}
	d.steps, d.tables = scan(d.lines)
	_, pickles, err := feature.ParseSource(uri, []byte(text), messages.UUID{}.NewId)
	if err != nil {
		d.parseErr = err
		return d
	}
	d.pickles = pickles
	for _, p := range pickles {
		for _, ps := range p.Steps {
			line := p.StepSource(ps).Line - 1
			if !contains(d.expansions[line], ps.Text) {
				d.expansions[line] = append(d.expansions[line], ps.Text)
			}
		}
	}
	return d
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// stepAt returns the step on line, if there is one.
func (d *document) stepAt(line int) (stepLine, bool) {
	i := sort.Search(len(d.steps), func(i int) bool { return d.steps[i].line >= line })
	if i < len(d.steps) && d.steps[i].line == line {
		return d.steps[i], true
	}
	return stepLine{}, false
}

// matchText is the text to match for a step: the step itself, or its first
// expansion for a Scenario Outline step.
func (d *document) matchText(st stepLine) string {
	if st.outline {
		if ex := d.expansions[st.line]; len(ex) > 0 {
			return ex[0]
		}
	}
	return st.text
}

// textRange is the range of a step's text.
func (d *document) textRange(st stepLine) textRange {
	return span(d.lines, st.line, st.textStart, st.textStart+len(st.text))
}

var languageHeader = regexp.MustCompile(`^\s*#\s*language\s*:\s*([\w-]+)\s*$`)

// scan finds the steps and tables of a feature file by reading it line by
// line, so they are found even while the file does not parse (a table row
// being typed, say).
func scan(lines []string) ([]stepLine, []table) {
	dialect := gherkin.DialectsBuiltin().GetDialect(gherkin.DefaultDialect)
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if m := languageHeader.FindStringSubmatch(l); m != nil {
			if d := gherkin.DialectsBuiltin().GetDialect(m[1]); d != nil {
				dialect = d
			}
		}
		if !strings.HasPrefix(t, "#") {
			break
		}
	}
	stepKeywords := append([]string(nil), dialect.StepKeywords()...)
	sort.Slice(stepKeywords, func(i, j int) bool { return len(stepKeywords[i]) > len(stepKeywords[j]) })
	header := func(t string, keywords []string) bool {
		for _, k := range keywords {
			if strings.HasPrefix(t, k+":") {
				return true
			}
		}
		return false
	}

	var steps []stepLine
	var tables []table
	fence := ""
	outline := false
	block := 0
	// owner is what a table starting on the next row belongs to: a step's
	// line, Examples, or nothing.
	const nothing, examples = -2, -1
	owner, inTable := nothing, false
	for i, l := range lines {
		t := strings.TrimLeft(l, " \t")
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		case strings.HasPrefix(t, `"""`):
			fence = `"""`
			owner, inTable = nothing, false
			continue
		case strings.HasPrefix(t, "```"):
			fence = "```"
			owner, inTable = nothing, false
			continue
		case strings.HasPrefix(t, "|"):
			if !inTable {
				if owner == nothing {
					continue
				}
				tables = append(tables, table{step: owner, block: block})
				inTable = true
			}
			tb := &tables[len(tables)-1]
			tb.rows = append(tb.rows, row{line: i, cells: splitRow(l)})
			continue
		case t == "" || strings.HasPrefix(t, "#"):
			continue
		}
		owner, inTable = nothing, false
		switch {
		case strings.HasPrefix(t, "@"):
			continue
		case header(t, dialect.ScenarioOutlineKeywords()):
			outline = true
			block++
			continue
		case header(t, dialect.ScenarioKeywords()), header(t, dialect.BackgroundKeywords()),
			header(t, dialect.RuleKeywords()), header(t, dialect.FeatureKeywords()):
			outline = false
			block++
			continue
		case header(t, dialect.ExamplesKeywords()):
			owner = examples
			continue
		}
		for _, kw := range stepKeywords {
			if !strings.HasPrefix(t, kw) {
				continue
			}
			start := len(l) - len(t) + len(kw)
			rest := l[start:]
			trimmed := strings.TrimLeft(rest, " \t")
			start += len(rest) - len(trimmed)
			steps = append(steps, stepLine{
				line: i, keyword: strings.TrimSpace(kw), textStart: start,
				text: strings.TrimRight(trimmed, " \t"), outline: outline, block: block,
			})
			owner = i
			break
		}
	}
	return steps, tables
}

// splitRow splits a table row at its unescaped pipes. Text after the last
// pipe is an open cell.
func splitRow(l string) []cell {
	var out []cell
	from := strings.IndexByte(l, '|') + 1
	escaped := false
	add := func(to int, open bool) {
		inner := l[from:to]
		text := strings.TrimFunc(inner, unicode.IsSpace)
		start := from + len(inner) - len(strings.TrimLeftFunc(inner, unicode.IsSpace))
		out = append(out, cell{
			text: text, from: start, to: start + len(text), innerFrom: from, innerTo: to,
			open: open, escaped: escaped,
		})
	}
	for j := from; j < len(l); j++ {
		switch l[j] {
		case '\\':
			escaped = true
			j++
		case '|':
			add(j, false)
			from, escaped = j+1, false
		}
	}
	add(len(l), true)
	return out
}

var parseErrorPattern = regexp.MustCompile(`\((\d+):(\d+)\): ([^\n]*)`)

// diagnostics are the problems of a document: syntax errors, steps that
// cannot run, and the warnings of axx lint's feature checks.
func (d *document) diagnostics(p *project) []diagnostic {
	out := []diagnostic{}
	if p.err != nil {
		out = append(out, diagnostic{
			Range: wholeLine(d.lines, 0), Severity: severityWarning, Source: "axx",
			Message: "axx cannot load this project's steps: " + p.err.Error(),
		})
	}
	if d.parseErr != nil {
		found := parseErrorPattern.FindAllStringSubmatch(d.parseErr.Error(), -1)
		for _, m := range found {
			line, _ := strconv.Atoi(m[1])
			out = append(out, diagnostic{
				Range: wholeLine(d.lines, line-1), Severity: severityError, Code: "syntax", Source: "axx", Message: m[3],
			})
		}
		if len(found) == 0 {
			out = append(out, diagnostic{Range: wholeLine(d.lines, 0), Severity: severityError, Code: "syntax", Source: "axx", Message: d.parseErr.Error()})
		}
		return out
	}
	if p.reg == nil {
		return out
	}
	for _, pr := range check.Pickles(p.reg, d.pickles).Problems {
		line := pr.Line - 1
		st, ok := d.stepAt(line)
		r := wholeLine(d.lines, line)
		if ok {
			r = d.textRange(st)
		}
		out = append(out, diagnostic{Range: r, Severity: severityError, Code: pr.Kind, Source: "axx", Message: problemMessage(pr, st.outline)})
	}
	out = append(out, d.fileDiagnostics(p)...)
	for _, f := range lint.CheckFeatures(p.reg, d.pickles, p.dir).Findings {
		if len(f.Locations) == 0 {
			continue
		}
		line := f.Locations[0].Line - 1
		r := wholeLine(d.lines, line)
		if st, ok := d.stepAt(line); ok {
			r = d.textRange(st)
		}
		out = append(out, diagnostic{Range: r, Severity: severityWarning, Code: f.Code, Source: "axx", Message: f.Message})
	}
	return out
}

func problemMessage(pr check.Problem, outline bool) string {
	var b strings.Builder
	switch pr.Kind {
	case check.Undefined:
		b.WriteString("Undefined step: no step matches this text.")
		if outline {
			fmt.Fprintf(&b, " With example values it reads %q.", pr.Text)
		}
		if len(pr.Suggestions) > 0 {
			b.WriteString("\nDid you mean:")
			for _, s := range pr.Suggestions {
				b.WriteString("\n  " + s.Expr)
			}
		}
	case check.Ambiguous:
		b.WriteString("Ambiguous step: more than one step matches.")
		for _, c := range pr.Candidates {
			b.WriteString("\n  " + c)
		}
	default:
		if pr.Message != "" {
			b.WriteString(strings.ToUpper(pr.Message[:1]) + pr.Message[1:] + ".")
		}
	}
	return b.String()
}

var placeholderPattern = regexp.MustCompile(`<[^<>\s][^<>]*>`)

// tokens are the semantic tokens of a document: the parameter values of
// each matched step, and the <placeholders> of Scenario Outline steps.
func (d *document) tokens(reg *match.Registry) []uint32 {
	type token struct{ line, from, to, kind int }
	var toks []token
	for _, st := range d.steps {
		if st.outline {
			for _, loc := range placeholderPattern.FindAllStringIndex(st.text, -1) {
				toks = append(toks, token{st.line, st.textStart + loc[0], st.textStart + loc[1], tokenVariable})
			}
			continue
		}
		if reg == nil {
			continue
		}
		ms := reg.Match(st.text)
		if len(ms) != 1 {
			continue
		}
		for _, a := range ms[0].Args {
			if !a.Present || a.Raw == "" {
				continue
			}
			toks = append(toks, token{st.line, st.textStart + a.Start, st.textStart + a.Start + len(a.Raw), tokenParameter})
		}
	}
	sort.Slice(toks, func(i, j int) bool {
		if toks[i].line != toks[j].line {
			return toks[i].line < toks[j].line
		}
		return toks[i].from < toks[j].from
	})
	data := make([]uint32, 0, len(toks)*5)
	prevLine, prevChar := 0, 0
	for _, t := range toks {
		r := span(d.lines, t.line, t.from, t.to)
		if r.End.Character <= r.Start.Character {
			continue
		}
		deltaChar := r.Start.Character
		if t.line == prevLine {
			deltaChar -= prevChar
		}
		data = append(data, uint32(t.line-prevLine), uint32(deltaChar), uint32(r.End.Character-r.Start.Character), uint32(t.kind), 0)
		prevLine, prevChar = t.line, r.Start.Character
	}
	return data
}
