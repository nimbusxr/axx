package rest_test

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// TestStepExamples checks, against every pack, that each REST step
// is documented and that each of its examples matches that step and no
// other (an ambiguous step fails at run time).
func TestStepExamples(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	keywords := []string{"Given ", "When ", "Then ", "And ", "But "}
	n := 0
	for _, d := range e.Registry.Defs() {
		if d.Pack != "rest" {
			continue
		}
		n++
		s := d.Step
		if s.Doc == "" || len(s.Examples) == 0 {
			t.Errorf("%s: needs Doc and Examples", s.ID)
		}
		for _, ex := range s.Examples {
			text := ex
			for _, kw := range keywords {
				text = strings.TrimPrefix(text, kw)
			}
			ms := e.Registry.Match(text)
			switch {
			case len(ms) != 1:
				t.Errorf("%s: example %q matches %d steps", s.ID, ex, len(ms))
			case ms[0].Def().Step.ID != s.ID:
				t.Errorf("%s: example %q matches %s", s.ID, ex, ms[0].Def().Step.ID)
			}
		}
	}
	if n == 0 {
		t.Fatal("the rest pack is not registered")
	}
}
