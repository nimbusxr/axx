package core

import (
	"fmt"
	"strings"
)

// Arg is one matched expression argument.
type Arg struct {
	// Param is the parameter type name ("int", "word", "service"...).
	Param string `json:"param"`
	// Present is false for arguments of an omitted optional [[...]] segment.
	Present bool `json:"present"`
	// Raw is the matched text, e.g. `'value'` for a {string}.
	Raw string `json:"raw,omitempty"`
	// Start is the byte offset of the match in the step text.
	Start int `json:"start,omitempty"`
	// Groups are the parameter regexp's capture groups.
	Groups []*string `json:"-"`
	// Value is the transformed value (set by the host before the step runs).
	Value any `json:"value,omitempty"`
}

// Args holds a step's arguments.
type Args struct {
	// Text is the full step text (without keyword).
	Text      string     `json:"text"`
	List      []Arg      `json:"args"`
	Table     *Table     `json:"dataTable,omitempty"`
	DocString *DocString `json:"docString,omitempty"`
}

func (a Args) at(i int) Arg {
	if i < 0 || i >= len(a.List) {
		panic(fmt.Sprintf("axx: argument %d out of range (step has %d)", i, len(a.List)))
	}
	return a.List[i]
}

// Len returns the number of argument slots in the full expression.
func (a Args) Len() int { return len(a.List) }

// Present reports whether argument i was matched.
func (a Args) Present(i int) bool { return i >= 0 && i < len(a.List) && a.List[i].Present }

// Value returns argument i's transformed value (nil if absent).
func (a Args) Value(i int) any { return a.at(i).Value }

// Raw returns argument i's matched text ("" if absent).
func (a Args) Raw(i int) string { return a.at(i).Raw }

// String returns argument i as a string. Panics if the value is not a string.
func (a Args) String(i int) string {
	arg := a.at(i)
	if !arg.Present {
		return ""
	}
	s, ok := arg.Value.(string)
	if !ok {
		panic(fmt.Sprintf("axx: argument %d ({%s}) is %T, not string", i, arg.Param, arg.Value))
	}
	return s
}

// Int returns argument i as an int. Panics if the value is not an int.
func (a Args) Int(i int) int {
	arg := a.at(i)
	n, ok := arg.Value.(int)
	if !ok {
		panic(fmt.Sprintf("axx: argument %d ({%s}) is %T, not int", i, arg.Param, arg.Value))
	}
	return n
}

// IntOr returns argument i as an int, or def when the argument is absent.
// Ordinals use IntOr(i, 1): an omitted ordinal means "the first".
func (a Args) IntOr(i, def int) int {
	if !a.Present(i) {
		return def
	}
	return a.Int(i)
}

// Table is a Gherkin data table.
type Table struct {
	Rows [][]string `json:"rows"`
}

// Pair is a row of a two-column table. Null is true when the value cell was
// empty (Cucumber-JVM converts empty cells to null).
type Pair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Null  bool   `json:"null,omitempty"`
}

// Pairs interprets a two-column table as ordered key/value pairs. Duplicate
// keys are an error, matching Cucumber-JVM's DataTable.asMap.
func (t *Table) Pairs() ([]Pair, error) {
	if t == nil {
		return nil, fmt.Errorf("a data table is required")
	}
	seen := make(map[string]bool, len(t.Rows))
	out := make([]Pair, 0, len(t.Rows))
	for i, row := range t.Rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("data table row %d has %d cells; expected 2 (key | value)", i+1, len(row))
		}
		if seen[row[0]] {
			return nil, fmt.Errorf("data table has duplicate key %q", row[0])
		}
		seen[row[0]] = true
		out = append(out, Pair{Key: row[0], Value: row[1], Null: row[1] == ""})
	}
	return out, nil
}

// Column returns the cells of column i of every row.
func (t *Table) Column(i int) []string {
	if t == nil {
		return nil
	}
	out := make([]string, 0, len(t.Rows))
	for _, row := range t.Rows {
		if i < len(row) {
			out = append(out, row[i])
		}
	}
	return out
}

// String renders the table in Gherkin syntax.
func (t *Table) String() string {
	if t == nil {
		return ""
	}
	widths := map[int]int{}
	for _, row := range t.Rows {
		for i, c := range row {
			widths[i] = max(widths[i], len(c))
		}
	}
	var b strings.Builder
	for _, row := range t.Rows {
		b.WriteString("|")
		for i, c := range row {
			fmt.Fprintf(&b, " %-*s |", widths[i], c)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// DocString is a Gherkin doc string argument.
type DocString struct {
	Content   string `json:"content"`
	MediaType string `json:"mediaType,omitempty"`
}
