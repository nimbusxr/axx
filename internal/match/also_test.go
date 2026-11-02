package match

import (
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

// A step's other wordings run the step: each parameter takes the argument of
// its type, whatever their order, and a text one of a type the step lacks the
// next text argument left. Lists tell them from the step's own variants.
func TestAlsoWordings(t *testing.T) {
	r := NewRegistry()
	if err := r.AddParams("t", []core.ParamType{{Name: "count", Regexps: []string{`\d+`}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.AddSteps("t", []core.StepDef{{
		ID:   "t.rows",
		Expr: "the[[ {float}]] selection[[ on {word}]] has {int} row(s)",
		Also: []string{"the[[ {float}]] selection has {int} row(s) on {word}"},
	}, {
		ID:   "t.says",
		Expr: "the {word} note says {string}",
		Also: []string{"the {word} note counts {count}"},
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
	// What reports show is in the text's order.
	if got := ms[0].InText(); len(got) != 2 || got[0].Raw != "2" || got[0].Start != 18 || got[1].Raw != "db" || got[1].Start != 28 {
		t.Errorf("in the text's order: %+v", got)
	}
	if ms := r.Match("the selection on db has 2 rows"); len(ms) != 1 || ms[0].Variant.Also {
		t.Errorf("the step's own wording: %+v", ms)
	}
	// {count} is no parameter of the step: it takes the text argument left, with its own type.
	ms = r.Match("the db note counts 12")
	if len(ms) != 1 {
		t.Fatalf("matches %+v", ms)
	}
	if a := ms[0].Args; a[0].Raw != "db" || a[1].Raw != "12" || a[1].Param != "count" {
		t.Errorf("args %+v", a)
	}
}

// A wording takes the same parameters as its step, or it does not register:
// a step's Run reads what its own forms take.
func TestAlsoWordingsThatCannotBind(t *testing.T) {
	for _, c := range []struct {
		name, expr string
		also       []string
		want       string
	}{
		{"more parameters", "a {int}", []string{"a {int} and {int}"}, "it has more parameters than the step"},
		{"an argument unbound", "wait {int} on {word}", []string{"wait on {word}"}, `"wait on {word}" leaves out {int}`},
		{"unbound in one form", "wait {int} on {word}", []string{"wait[[ {int}]] on {word}"}, `"wait on {word}" leaves out {int}`},
		{"part of an optional part", "go[[ {int} on {word}]]", []string{"go {int}"}, `"go {int}" leaves out {word}`},
		{"a word for an int", "wait {int} on {word}", []string{"wait {word} {word}"}, "its {word} cannot take the place of the step's {int}"},
		{"an int for a word", "wait on {word}", []string{"wait on {int}"}, "its {int} cannot take the place of the step's {word}"},
		{"a word for a type that transforms", "pick {thing}", []string{"take {word}"}, "its {word} cannot take the place of the step's {thing}"},
		{"a type that transforms for a string", "say {string}", []string{"say {thing}"}, "its {thing} cannot take the place of the step's {string}"},
		{"what the step says", "go[[ now]]", []string{"go now"}, `the step already says "go now"`},
		{"said twice", "go", []string{"leave", "leave"}, `the step already says "leave"`},
		{"said twice in forms", "go", []string{"leave[[ now]]", "leave now"}, `the step already says "leave now"`},
		{"an unknown type", "go {word}", []string{"go {nothing}"}, "nothing"},
	} {
		r := NewRegistry()
		if err := r.AddParams("t", []core.ParamType{{
			Name: "thing", Regexps: []string{`T\d`},
			Transform: func(*core.Scenario, string, []*string) (any, error) { return 1, nil },
		}}); err != nil {
			t.Fatal(err)
		}
		err := r.AddSteps("t", []core.StepDef{{ID: "t.s", Expr: c.expr, Also: c.also}})
		if err == nil || !strings.Contains(err.Error(), c.want) || !strings.HasPrefix(err.Error(), "t: step t.s: ") {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
	// An optional parameter of the step may stay out, in a form it may stay out of.
	r := NewRegistry()
	if err := r.AddSteps("t", []core.StepDef{{ID: "t.s", Expr: "wait[[ {int}]] on {word}", Also: []string{"on {word} wait", "on {word} wait {int}"}}}); err != nil {
		t.Errorf("an optional parameter: %v", err)
	}
	if ms := r.Match("on db wait"); len(ms) != 1 || ms[0].Args[0].Present || ms[0].Args[1].Raw != "db" {
		t.Errorf("the wording without it: %+v", ms)
	}
}

// Parameters of one type bind in the order they are written: registration
// cannot see what a wording's words mean, so StepDef.Also tells authors to keep
// that order. A wording that does not has them swapped.
func TestAlsoWordingsBindOneTypeInOrder(t *testing.T) {
	r := NewRegistry()
	if err := r.AddSteps("t", []core.StepDef{{ID: "t.copy", Expr: "copy {word} to {word}", Also: []string{"to {word} copy {word}"}}}); err != nil {
		t.Fatal(err)
	}
	ms := r.Match("to target copy source")
	if len(ms) != 1 {
		t.Fatalf("matches %+v", ms)
	}
	var got []string
	for _, a := range ms[0].Args {
		got = append(got, a.Raw)
	}
	if !slices.Equal(got, []string{"target", "source"}) {
		t.Errorf("args %v", got)
	}
}
