package filecontent

import (
	"encoding/csv"
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
)

// Table is a table a file holds: the names of its columns (its first row)
// and the rows below them.
type Table struct {
	Columns []string
	Rows    [][]string
}

// HasTable reports whether files of the kind hold a table axx reads: CSV,
// TSV and Excel files, and text files, read as CSV.
func (k Kind) HasTable() bool { return k == CSV || k == TSV || k == Excel || k == Text }

// Table reads the table a CSV, TSV or Excel file holds; of a workbook, the
// named sheet, or else the first one. A text file of no known kind is read
// as CSV.
func (f File) Table(sheet string) (*Table, error) {
	k := f.Kind()
	if sheet != "" && k != Excel {
		return nil, fmt.Errorf("it is %s, which has no sheets", k.Noun())
	}
	switch k {
	case Excel:
		return excelTable(f.Body, sheet)
	case CSV, Text:
		return csvTable(decode(f.Body), 0)
	case TSV:
		return csvTable(decode(f.Body), '\t')
	}
	return nil, fmt.Errorf("it is %s, not a table axx reads (CSV, TSV, Excel .xlsx)", k.Noun())
}

// csvTable reads CSV text. Without a separator, it is the one an Excel
// "sep=" line names, or else the one of comma, semicolon and tab the first
// line has most of.
func csvTable(text string, sep rune) (*Table, error) {
	if s, ok := strings.CutPrefix(text, "sep="); ok && len(s) > 1 && (s[1] == '\n' || s[1] == '\r') {
		sep = rune(s[0])
		text = strings.TrimLeft(s[1:], "\r\n")
	}
	if sep == 0 {
		first, _, _ := strings.Cut(text, "\n")
		sep = ','
		for _, c := range []rune{';', '\t'} {
			if strings.Count(first, string(c)) > strings.Count(first, string(sep)) {
				sep = c
			}
		}
	}
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = sep
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			return nil, fmt.Errorf("cannot read the table: line %d: %w", pe.Line, pe.Err)
		}
		return nil, fmt.Errorf("cannot read the table: %w", err)
	}
	return newTable(records), nil
}

// newTable makes a table of records: the first one that is not empty names
// the columns, and empty ones are left out.
func newTable(records [][]string) *Table {
	t := &Table{}
	for _, r := range records {
		empty := true
		for i := range r {
			r[i] = Collapse(r[i])
			if r[i] != "" {
				empty = false
			}
		}
		switch {
		case empty:
		case t.Columns == nil:
			t.Columns = r
		default:
			t.Rows = append(t.Rows, r)
		}
	}
	return t
}

// FindRow reports whether a row has, in each column want names, the value
// it gives: compared as text, runs of spaces and line breaks as one space,
// an empty value for an empty cell. When no row has, why says what the
// table has instead.
func (t *Table) FindRow(want []core.Pair) (ok bool, why string) {
	if t.Columns == nil {
		return false, "it holds no table: it is empty"
	}
	idx := make([]int, len(want))
	var missing []string
	for i, p := range want {
		idx[i] = -1
		for j, c := range t.Columns {
			if c == Collapse(p.Key) {
				idx[i] = j
				break
			}
		}
		if idx[i] < 0 {
			missing = append(missing, fmt.Sprintf("%q", p.Key))
		}
	}
	if len(missing) > 0 {
		noun := "column"
		if len(missing) > 1 {
			noun = "columns"
		}
		return false, fmt.Sprintf("its table has no %s %s; its columns are: %s", strings.Join(missing, ", "), noun, strings.Join(t.Columns, ", "))
	}
	if len(t.Rows) == 0 {
		return false, "its table has no rows, only the columns " + strings.Join(t.Columns, ", ")
	}
	var seen []string
	for _, r := range t.Rows {
		match := true
		parts := make([]string, len(want))
		for i, p := range want {
			cell := ""
			if idx[i] < len(r) {
				cell = r[idx[i]]
			}
			parts[i] = p.Key + "=" + cell
			if cell != Collapse(p.Value) {
				match = false
			}
		}
		if match {
			return true, ""
		}
		seen = append(seen, strings.Join(parts, ", "))
	}
	wanted := make([]string, len(want))
	for i, p := range want {
		wanted[i] = p.Key + "=" + Collapse(p.Value)
	}
	n := len(seen)
	if n > 10 {
		seen = append(seen[:10], fmt.Sprintf("… and %d more", n-10))
	}
	rows := "rows are"
	if n == 1 {
		rows = "row is"
	}
	return false, fmt.Sprintf("no row has %s. Its %d %s:\n  %s", strings.Join(wanted, ", "), n, rows, strings.Join(seen, "\n  "))
}
