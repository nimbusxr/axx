package check

import (
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
