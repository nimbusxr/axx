package engine

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"

	"github.com/nimbusxr/axx/packs/all"
)

const catalogFile = "../../testdata/steps.json"

var (
	updateCatalog = flag.Bool("update", false, "add the steps and parameter types missing from testdata/steps.json, and record the pack a step moved to; never removes an entry")
	pruneCatalog  = flag.Bool("prune", false, "remove the entries of steps no pack defines any more: for steps the owner decided to remove, never to hide one that went missing")
)

type catalog struct {
	Description string         `json:"description"`
	Counts      map[string]int `json:"counts"`
	Entries     []catalogEntry `json:"entries"`
}

type catalogEntry struct {
	Kind       string `json:"kind"`
	Pack       string `json:"pack"`
	Keyword    string `json:"keyword"`
	Name       string `json:"name,omitempty"`
	Expression string `json:"expression"`
}

// TestStepCatalog asserts that every step expression and parameter type of
// the frozen catalog (testdata/steps.json) is defined, verbatim, by its
// pack: step text is public API and never changes by accident. And that the
// catalog has every step and parameter type of axx's packs, so that new ones
// are frozen too: -update adds them. A step the owner decides to remove is
// taken out with -prune.
func TestStepCatalog(t *testing.T) {
	b, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	var cat catalog
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
	listed := map[string]bool{}
	kept := make([]catalogEntry, 0, len(cat.Entries))
	pruned, moved := 0, 0
	for _, c := range cat.Entries {
		switch c.Kind {
		case "step":
			got, ok := exprs[c.Expression]
			if !ok && *pruneCatalog {
				pruned++
				continue
			}
			if ok && got != c.Pack && *updateCatalog {
				// The same text from another pack, like one its pack builds on.
				c.Pack = got
				moved++
			}
			kept = append(kept, c)
			listed["step "+c.Expression] = true
			if !ok {
				t.Errorf("step %q is missing from pack %s: step text is public API; if its removal is intended, "+
					"run `go test ./internal/engine -run TestStepCatalog -prune`", c.Expression, c.Pack)
				continue
			}
			if got != c.Pack {
				t.Errorf("step %q is provided by pack %s, want %s: if it moved on purpose, to a pack every user of %s has, "+
					"run `go test ./internal/engine -run TestStepCatalog -update`", c.Expression, got, c.Pack, c.Pack)
			}
		case "parameterType":
			kept = append(kept, c)
			listed["parameterType "+c.Name] = true
			re, ok := params[c.Name]
			if !ok || len(re) == 0 || re[0] != c.Expression {
				t.Errorf("parameter type {%s} with regexp %q is missing or differs (have %q)", c.Name, c.Expression, re)
			}
		}
	}

	var missing []catalogEntry
	for _, p := range e.Registry.Params() {
		if p.Pack == "cucumber" || listed["parameterType "+p.Type.Name] {
			continue
		}
		missing = append(missing, catalogEntry{Kind: "parameterType", Pack: p.Pack, Keyword: "ParameterType", Name: p.Type.Name, Expression: p.Type.Regexps[0]})
	}
	for _, v := range e.Registry.Variants() {
		if listed["step "+v.Expr] {
			continue
		}
		listed["step "+v.Expr] = true
		missing = append(missing, catalogEntry{Kind: "step", Pack: v.Def.Pack, Keyword: v.Def.Step.Keyword, Expression: v.Expr})
	}
	if (*updateCatalog && len(missing)+moved > 0) || pruned > 0 {
		cat.Entries = kept
		if *updateCatalog {
			cat.Entries = append(cat.Entries, missing...)
		}
		cat.Counts = map[string]int{}
		for _, c := range cat.Entries {
			cat.Counts[c.Kind]++
		}
		var out bytes.Buffer
		enc := json.NewEncoder(&out)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(cat); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(catalogFile, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: added %d entries, moved %d, removed %d", catalogFile, len(missing), moved, pruned)
		return
	}
	for _, m := range missing {
		t.Errorf("%s %q of pack %s is not in the frozen catalog: run `go test ./internal/engine -run TestStepCatalog -update` to add it", m.Kind, m.Name+m.Expression, m.Pack)
	}
	counts := map[string]int{}
	for _, c := range cat.Entries {
		counts[c.Kind]++
	}
	for kind, n := range counts {
		if cat.Counts[kind] != n {
			t.Errorf("the catalog counts %d %s entries, but lists %d", cat.Counts[kind], kind, n)
		}
	}
}
