package match

import (
	"testing"

	"github.com/nimbusxr/axx/core"
)

// A step's other wordings run the step: each parameter takes the argument of
// its type, whatever their order, and one of a type the step lacks the next
// argument left. Lists tell them from the step's own variants.
func TestAlsoWordings(t *testing.T) {
	r := NewRegistry()
	if err := r.AddSteps("t", []core.StepDef{{
		ID:   "t.rows",
		Expr: "the[[ {float}]] selection[[ on {word}]] has {int} row(s)",
		Also: []string{
			"the[[ {float}]] selection has {int} row(s) on {word}",
			"the {word} selection counts {string}",
		},
	}}); err != nil {
		t.Fatal(err)
	}
	ms := r.Match("the selection has 2 rows on db")
	if len(ms) != 1 || !ms[0].Variant.Also {
		t.Fatalf("matches %+v", ms)
	}
	a := ms[0].Args // the step's order: float, word, int
	if a[0].Present || a[1].Raw != "db" || a[2].Raw != "2" {
		t.Errorf("args %+v", a)
	}
	if ms := r.Match("the selection on db has 2 rows"); len(ms) != 1 || ms[0].Variant.Also {
		t.Errorf("the step's own wording: %+v", ms)
	}
	// {string} is no parameter of the step: it takes what is left, with its own type.
	ms = r.Match("the db selection counts 'many'")
	if len(ms) != 1 {
		t.Fatalf("matches %+v", ms)
	}
	if a := ms[0].Args; a[1].Raw != "db" || !a[0].Present || a[0].Param != "string" {
		t.Errorf("args %+v", a)
	}
	if err := r.AddSteps("t", []core.StepDef{{ID: "t.more", Expr: "a {int}", Also: []string{"a {int} and {int}"}}}); err == nil {
		t.Error("a wording with more parameters than its step must not register")
	}
}
