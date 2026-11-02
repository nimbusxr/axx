package check

import (
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/internal/match"
)

func TestPickles(t *testing.T) {
	reg := match.NewRegistry()
	if err := reg.AddPack("t", core.Manifest{Name: "t", Steps: []core.StepDef{
		{ID: "t.count", Expr: "the count is {int}"},
		{ID: "t.table", Expr: "the rows are:", Arg: core.ArgTable},
		{ID: "t.a", Expr: "a {word}"},
		{ID: "t.b", Expr: "a thing"},
	}}); err != nil {
		t.Fatal(err)
	}
	src := `Feature: f
  Scenario Outline: o
    Then the count is <n>
    Examples:
      | n   |
      | 1   |
      | two |

  Scenario: s
    Then the rows are:
    And a thing
`
	_, pickles, err := feature.ParseSource("f.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	res := Pickles(reg, pickles)
	if res.Steps != 4 {
		t.Errorf("steps %d", res.Steps)
	}
	// The second example row makes the outline step undefined, even though
	// the first row matches.
	want := []struct {
		kind string
		line int
		text string
	}{
		{Undefined, 3, "the count is two"},
		{Argument, 10, "the rows are:"},
		{Ambiguous, 11, "a thing"},
	}
	if len(res.Problems) != len(want) {
		t.Fatalf("problems %+v", res.Problems)
	}
	for i, w := range want {
		p := res.Problems[i]
		if p.Kind != w.kind || p.Line != w.line || p.Text != w.text {
			t.Errorf("problem %d: %+v, want %+v", i, p, w)
		}
	}
}

// A value cell the rule cannot read is a problem at its row (ADR 0014).
func TestPicklesValues(t *testing.T) {
	reg := match.NewRegistry()
	if err := reg.AddPack("t", core.Manifest{Name: "t", Steps: []core.StepDef{
		{
			ID: "t.props", Expr: "the properties are:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"JSONPath", "value"}, Values: core.JSONValues, Undefined: true},
		},
		{
			ID: "t.headers", Expr: "the headers are:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"header", "value"}, Values: core.TextValues},
		},
		{
			ID: "t.event", Expr: "the event is:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Values: core.JSONValues, Undefined: true, TextRows: []string{"event <name>"}},
		},
		{
			ID: "t.settings", Expr: "the settings are:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"property", "value"}},
		},
	}}); err != nil {
		t.Fatal(err)
	}
	src := `Feature: f
  Scenario: s
    Then the properties are:
      | JSONPath | value     |
      | postcode | 01067     |
      | weight   | 1200      |
      | note     |           |
      | gone     | undefined |
    And the headers are:
      | X-Count | 3   |
      | X-Ref   | "3" |
    And the event is:
      | event id | 1 |
      | count    | 1 |
    And the settings are:
      | timeout | 01067 |
`
	_, pickles, err := feature.ParseSource("f.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	res := Pickles(reg, pickles)
	want := []struct {
		line int
		msg  string
	}{
		{5, `postcode: 01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`},
		{7, `note: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`},
		{10, `X-Count: 3 reads as an integer, and only text fits here: write "3" for the text`},
		{13, `event id: 1 reads as an integer, and only text fits here: write "1" for the text`},
	}
	if len(res.Problems) != len(want) {
		t.Fatalf("problems %+v", res.Problems)
	}
	for i, w := range want {
		if p := res.Problems[i]; p.Kind != Value || p.Line != w.line || p.Message != w.msg {
			t.Errorf("problem %d: %+v, want line %d: %s", i, p, w.line, w.msg)
		}
	}
}

func valueRegistry(t *testing.T) *match.Registry {
	t.Helper()
	reg := match.NewRegistry()
	if err := reg.AddPack("t", core.Manifest{Name: "t", Steps: []core.StepDef{
		{
			ID: "t.props", Expr: "the properties are:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"JSONPath", "value"}, Values: core.JSONValues, Undefined: true},
		},
		{
			ID: "t.text", Expr: "the text is:", Arg: core.ArgTable,
			Table: &core.TableDoc{Columns: []string{"name", "value"}, Values: core.TextValues},
		},
		{
			ID: "t.message", Expr: "the message is:", Arg: core.ArgTable,
			Table: &core.TableDoc{
				Columns: []string{"path", "value"}, Values: core.JSONValues, Undefined: true,
				TextRows: []string{"attribute <name>", "routing key"},
			},
		},
		{ID: "t.lines", Expr: "the lines are:", Arg: core.ArgTable},
	}}); err != nil {
		t.Fatal(err)
	}
	return reg
}

func picklesOf(t *testing.T, src string) []*feature.Pickle {
	t.Helper()
	_, pickles, err := feature.ParseSource("f.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	return pickles
}

func valueMessages(t *testing.T, src string) []string {
	t.Helper()
	got := []string{}
	for _, p := range Pickles(valueRegistry(t), picklesOf(t, src)).Problems {
		if p.Kind != Value || p.Code != ValueCode {
			t.Errorf("not a value problem: %+v", p)
		}
		got = append(got, p.Message)
	}
	return got
}

func sameMessages(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("problems\n got %q\nwant %q", got, want)
	}
}

// An outline's examples give a table its values, so each is checked, as the
// run reads each, and what they share is reported once.
func TestOutlineTablesAreCheckedForEachExample(t *testing.T) {
	sameMessages(t, valueMessages(t, `Feature: f
  Scenario Outline: o
    Then the properties are:
      | n | <v>   |
      | m | 01067 |
    Examples:
      | v   |
      | 42  |
      | 042 |
      |     |
      | 042 |
`), []string{
		`m: 01067 could be the number 1067 or the text 01067: write 1067 for the number, or "01067" for the text`,
		`n: 042 could be the number 42 or the text 042: write 42 for the number, or "042" for the text`,
		`n: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`,
	})
	// A step that cannot run, whatever its table, is one problem.
	res := Pickles(valueRegistry(t), picklesOf(t, `Feature: f
  Scenario Outline: o
    Then no such step:
      | n | <v> |
    Examples:
      | v |
      | 1 |
      | 2 |
`))
	if len(res.Problems) != 1 || res.Problems[0].Kind != Undefined {
		t.Errorf("problems %+v", res.Problems)
	}
	if res.Steps != 2 {
		t.Errorf("steps %d", res.Steps)
	}
}

// A cell holding a line break is not empty: the run reads it as a string.
func TestOnlyAnEmptyCellIsEmpty(t *testing.T) {
	sameMessages(t, valueMessages(t, `Feature: f
  Scenario: s
    Then the properties are:
      | a | \n |
      | b |    |
    And the text is:
      | c | \n |
`), []string{
		`b: the cell is empty: write "" for the empty string, null for null, or undefined for no such property`,
	})
	pickles := picklesOf(t, "Feature: f\n  Scenario: s\n    Then the properties are:\n      | a | \\n |\n")
	if got := pickles[0].Steps[0].Argument.DataTable.Rows[0].Cells[1].Value; got != "\n" {
		t.Errorf("the cell is %q", got)
	}
}

// A bare key of named rows, or more words after a row's name, is a path into
// the body, as the run reads it.
func TestTextRowsAreTheRowsTheRunReadsAsText(t *testing.T) {
	sameMessages(t, valueMessages(t, `Feature: f
  Scenario: s
    Then the message is:
      | attribute         | 3 |
      | routing key extra | 3 |
      | attribute x       | 3 |
      | routing key       | 3 |
`), []string{
		`attribute x: 3 reads as an integer, and only text fits here: write "3" for the text`,
		`routing key: 3 reads as an integer, and only text fits here: write "3" for the text`,
	})
}
