package match

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
)

func TestExpandVariants(t *testing.T) {
	vars, names, err := expandVariants("the {ordinal} row[[ for {ordinal} ordered response]][[ on {service}]] is {string}")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "ordinal,ordinal,service,string" {
		t.Fatalf("names = %v", names)
	}
	want := map[string][]int{
		"the {ordinal} row is {string}":                                             {0, 3},
		"the {ordinal} row for {ordinal} ordered response is {string}":              {0, 1, 3},
		"the {ordinal} row on {service} is {string}":                                {0, 2, 3},
		"the {ordinal} row for {ordinal} ordered response on {service} is {string}": {0, 1, 2, 3},
	}
	if len(vars) != len(want) {
		t.Fatalf("got %d variants, want %d", len(vars), len(want))
	}
	for _, v := range vars {
		pos, ok := want[v.expr]
		if !ok {
			t.Errorf("unexpected variant %q", v.expr)
			continue
		}
		if len(pos) != len(v.argPos) {
			t.Errorf("%q argPos = %v, want %v", v.expr, v.argPos, pos)
			continue
		}
		for i := range pos {
			if pos[i] != v.argPos[i] {
				t.Errorf("%q argPos = %v, want %v", v.expr, v.argPos, pos)
			}
		}
	}
	if _, _, err := expandVariants("a [[b [[c]] d]]"); err == nil {
		t.Error("nested segments must be rejected")
	}
	if _, _, err := expandVariants("a [[b"); err == nil {
		t.Error("unterminated segment must be rejected")
	}
}

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	must(t, r.AddParams("core", []core.ParamType{
		{Name: "ordinal", Regexps: []string{`(\d+)(?:st|nd|rd|th)`}, Transform: func(_ *core.Scenario, _ string, g []*string) (any, error) {
			return len(*g[0]), nil // stand-in transform: proves groups are passed
		}},
		{Name: "service", Regexps: []string{`([^\s]+)`}},
	}))
	must(t, r.AddSteps("rest", []core.StepDef{
		{ID: "status", Expr: "the[[ {ordinal} ordered]] response status code is {int}[[ on {service}]]"},
		{ID: "prop", Expr: "the response payload property {word} is {string}"},
		{ID: "req", Expr: "a(n) {word} request to {word}"},
	}))
	return r
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestMatchVariantsAndArgs(t *testing.T) {
	r := testRegistry(t)
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)

	ms := r.Match("the 12th ordered response status code is 201 on api")
	if len(ms) != 1 || ms[0].Def().Step.ID != "status" {
		t.Fatalf("matches = %+v", ms)
	}
	args, err := r.Resolve(sc, ms[0], "", nil, nil)
	must(t, err)
	if !args.Present(0) || args.Int(0) != 2 || args.Int(1) != 201 || args.String(2) != "api" {
		t.Fatalf("args = %+v", args.List)
	}

	ms = r.Match("the response status code is 200")
	if len(ms) != 1 {
		t.Fatalf("short form should match once, got %d", len(ms))
	}
	args, err = r.Resolve(sc, ms[0], "", nil, nil)
	must(t, err)
	if args.Present(0) || args.IntOr(0, 1) != 1 || args.Int(1) != 200 || args.Present(2) {
		t.Fatalf("absent optional args must report !Present: %+v", args.List)
	}

	if len(r.Match("an OPTIONS request to /api")) != 1 || len(r.Match("a GET request to /api?q=1")) != 1 {
		t.Error("optional text a(n) must match both a and an")
	}
	if len(r.Match("the response status code is two hundred")) != 0 {
		t.Error("non-integer must not match {int}")
	}
}

func TestStringParameter(t *testing.T) {
	r := testRegistry(t)
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
	cases := map[string]string{
		`the response payload property name is 'super duper'`: "super duper",
		`the response payload property name is "super duper"`: "super duper",
		`the response payload property name is '"123"'`:       `"123"`,
		`the response payload property name is 'it\'s'`:       "it's",
		`the response payload property name is "say \"hi\""`:  `say "hi"`,
		`the response payload property name is ''`:            "",
	}
	for text, want := range cases {
		ms := r.Match(text)
		if len(ms) != 1 {
			t.Errorf("%s: %d matches", text, len(ms))
			continue
		}
		args, err := r.Resolve(sc, ms[0], text, nil, nil)
		must(t, err)
		if got := args.String(1); got != want {
			t.Errorf("%s: got %q, want %q", text, got, want)
		}
	}
}

func TestIntOverflowIsTransformError(t *testing.T) {
	r := testRegistry(t)
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{}, nil, nil)
	ms := r.Match("the response status code is 99999999999")
	if len(ms) != 1 {
		t.Fatal("should match")
	}
	if _, err := r.Resolve(sc, ms[0], "", nil, nil); err == nil || !strings.Contains(err.Error(), "failed to transform") {
		t.Fatalf("want transform error, got %v", err)
	}
}

func TestDuplicateParamAndIDRejected(t *testing.T) {
	r := testRegistry(t)
	if err := r.AddParams("x", []core.ParamType{{Name: "service", Regexps: []string{"x"}}}); err == nil {
		t.Error("duplicate parameter type must be rejected")
	}
	if err := r.AddSteps("x", []core.StepDef{{ID: "status", Expr: "whatever"}}); err == nil {
		t.Error("duplicate step ID must be rejected")
	}
	if err := r.AddSteps("x", []core.StepDef{{ID: "bad", Expr: "uses {nope}"}}); err == nil {
		t.Error("undefined parameter type must be rejected")
	}
}

func TestAmbiguity(t *testing.T) {
	r := testRegistry(t)
	must(t, r.AddSteps("other", []core.StepDef{{ID: "dup", Expr: "the response status code is {int}"}}))
	if n := len(r.Match("the response status code is 200")); n != 2 {
		t.Fatalf("want ambiguous (2 matches), got %d", n)
	}
}

func TestSuggest(t *testing.T) {
	r := testRegistry(t)
	s := r.Suggest("the response status is 200", 3)
	if len(s) == 0 || s[0].ID != "status" {
		t.Fatalf("suggestions = %+v", s)
	}
	if s := r.Suggest("completely unrelated gibberish words here now", 3); len(s) != 0 {
		t.Errorf("unrelated text should not produce suggestions: %+v", s)
	}
}

// TestCatalogCompiles checks that every expression and parameter type of
// the frozen step catalog compiles with the Go Cucumber Expressions library
// and is unambiguous against itself.
func TestCatalogCompiles(t *testing.T) {
	b, err := os.ReadFile("../../testdata/steps.json")
	if err != nil {
		t.Fatal(err)
	}
	var cat struct {
		Entries []struct {
			Kind       string `json:"kind"`
			Pack       string `json:"pack"`
			Name       string `json:"name"`
			Expression string `json:"expression"`
		} `json:"entries"`
	}
	must(t, json.Unmarshal(b, &cat))
	r := NewRegistry()
	var params []core.ParamType
	var steps []core.StepDef
	for i, e := range cat.Entries {
		switch e.Kind {
		case "parameterType":
			params = append(params, core.ParamType{Name: e.Name, Regexps: []string{e.Expression}})
		case "step":
			steps = append(steps, core.StepDef{ID: fmt.Sprintf("%s.%d", e.Pack, i), Expr: e.Expression})
		}
	}
	must(t, r.AddParams("catalog", params))
	must(t, r.AddSteps("catalog", steps))
	if got := len(r.Defs()); got != 219 {
		t.Fatalf("compiled %d catalog steps, want 219", got)
	}
}
