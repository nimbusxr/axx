package v8cov

import (
	"fmt"
	"strconv"
	"strings"
)

// The reports, written as istanbul-reports writes them: lcovonly,
// json (coverage-final.json) and json-summary (coverage-summary.json). A
// report lists files in the order given: Istanbul's is by path.

// Lcov is the files' coverage in lcov's format; SF is each file's path.
func Lcov(files []*FileCoverage) []byte {
	var b strings.Builder
	for _, fc := range files {
		sum := fc.Summary()
		b.WriteString("TN:\nSF:" + fc.Path + "\n")
		for _, fn := range fc.FnMap {
			fmt.Fprintf(&b, "FN:%d,%s\n", fn.Decl.Start.Line, fn.Name)
		}
		fmt.Fprintf(&b, "FNF:%d\nFNH:%d\n", sum.Functions.Total, sum.Functions.Covered)
		for i, fn := range fc.FnMap {
			fmt.Fprintf(&b, "FNDA:%d,%s\n", fc.F[i], fn.Name)
		}
		lines, hits := fc.LineCoverage()
		for _, l := range lines {
			fmt.Fprintf(&b, "DA:%d,%d\n", l, hits[l])
		}
		fmt.Fprintf(&b, "LF:%d\nLH:%d\n", sum.Lines.Total, sum.Lines.Covered)
		for i, hits := range fc.B {
			for j, h := range hits {
				fmt.Fprintf(&b, "BRDA:%d,%d,%d,%d\n", fc.BranchMap[i].Loc.Start.Line, i, j, h)
			}
		}
		fmt.Fprintf(&b, "BRF:%d\nBRH:%d\nend_of_record\n", sum.Branches.Total, sum.Branches.Covered)
	}
	return []byte(b.String())
}

// FinalJSON is the files' coverage as Istanbul's JSON, by path.
func FinalJSON(files []*FileCoverage) []byte {
	var b strings.Builder
	b.WriteString("{")
	for i, fc := range files {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(jsString(fc.Path) + ": ")
		fc.writeJSON(&b)
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// SummaryJSON is the summary of the files' coverage, their total first,
// then each file's, by path.
func SummaryJSON(files []*FileCoverage) []byte {
	var b strings.Builder
	b.WriteString("{")
	t := Total(files)
	b.WriteString(`"total": {"lines":` + t.Lines.json() + `,"statements":` + t.Statements.json() +
		`,"functions":` + t.Functions.json() + `,"branches":` + t.Branches.json() +
		`,"branchesTrue":` + Totals{}.json() + "}\n")
	for _, fc := range files {
		s := fc.Summary()
		b.WriteString("," + jsString(fc.Path) + `: {"lines":` + s.Lines.json() + `,"functions":` + s.Functions.json() +
			`,"statements":` + s.Statements.json() + `,"branches":` + s.Branches.json() + "}\n")
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func (t Totals) json() string {
	pct := `"Unknown"`
	if t.Pct != nil {
		pct = jsNumber(*t.Pct)
	}
	return fmt.Sprintf(`{"total":%d,"covered":%d,"skipped":%d,"pct":%s}`, t.Total, t.Covered, t.Skipped, pct)
}

func (fc *FileCoverage) writeJSON(b *strings.Builder) {
	b.WriteString(`{"path":` + jsString(fc.Path) + `,"all":` + strconv.FormatBool(fc.All) + `,"statementMap":{`)
	for i, l := range fc.StatementMap {
		key(b, i)
		b.WriteString(l.json())
	}
	b.WriteString(`},"s":{`)
	for i, n := range fc.S {
		key(b, i)
		b.WriteString(strconv.Itoa(n))
	}
	b.WriteString(`},"branchMap":{`)
	for i, m := range fc.BranchMap {
		key(b, i)
		b.WriteString(`{"type":` + jsString(m.Type) + `,"line":` + strconv.Itoa(m.Line) + `,"loc":` + m.Loc.json() + `,"locations":[`)
		for j, l := range m.Locations {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString(l.json())
		}
		b.WriteString("]}")
	}
	b.WriteString(`},"b":{`)
	for i, hits := range fc.B {
		key(b, i)
		b.WriteString("[")
		for j, h := range hits {
			if j > 0 {
				b.WriteString(",")
			}
			b.WriteString(strconv.Itoa(h))
		}
		b.WriteString("]")
	}
	b.WriteString(`},"fnMap":{`)
	for i, m := range fc.FnMap {
		key(b, i)
		b.WriteString(`{"name":` + jsString(m.Name) + `,"decl":` + m.Decl.json() + `,"loc":` + m.Loc.json() + `,"line":` + strconv.Itoa(m.Line) + "}")
	}
	b.WriteString(`},"f":{`)
	for i, n := range fc.F {
		key(b, i)
		b.WriteString(strconv.Itoa(n))
	}
	b.WriteString("}}")
}

func key(b *strings.Builder, i int) {
	if i > 0 {
		b.WriteString(",")
	}
	b.WriteString(`"` + strconv.Itoa(i) + `":`)
}

func (l Loc) json() string {
	return fmt.Sprintf(`{"start":{"line":%d,"column":%d},"end":{"line":%d,"column":%d}}`,
		l.Start.Line, l.Start.Column, l.End.Line, l.End.Column)
}

// jsNumber is a number as JavaScript writes it (the percentages Istanbul
// works out).
func jsNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// jsString is a string as JSON.stringify writes it.
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
