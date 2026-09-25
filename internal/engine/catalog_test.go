package engine

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/nimbusxr/axx/packs/all"
)

// TestStepCatalog asserts that every step expression and parameter type of
// the frozen catalog (testdata/steps.json) is defined, verbatim, by its
// pack: step text is public API and never changes.
func TestStepCatalog(t *testing.T) {
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
	if err := json.Unmarshal(b, &cat); err != nil {
		t.Fatal(err)
	}
	e, err := New(Options{Packs: Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	exprs := map[string]string{} // expression -> pack
	for _, v := range e.Registry.Variants() {
		exprs[v.Expr] = v.Def.Pack
	}
	params := map[string][]string{}
	for _, p := range e.Registry.Params() {
		params[p.Type.Name] = p.Type.Regexps
	}
	steps := 0
	for _, c := range cat.Entries {
		switch c.Kind {
		case "step":
			steps++
			got, ok := exprs[c.Expression]
			if !ok {
				t.Errorf("step %q is missing from pack %s", c.Expression, c.Pack)
				continue
			}
			if got != c.Pack {
				t.Errorf("step %q is provided by pack %s, want %s", c.Expression, got, c.Pack)
			}
		case "parameterType":
			re, ok := params[c.Name]
			if !ok || len(re) == 0 || re[0] != c.Expression {
				t.Errorf("parameter type {%s} with regexp %q is missing or differs (have %q)", c.Name, c.Expression, re)
			}
		}
	}
	if steps != 219 {
		t.Errorf("the catalog lists %d steps, want 219", steps)
	}
}
