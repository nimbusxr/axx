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
