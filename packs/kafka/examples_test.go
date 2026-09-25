package kafka_test

import (
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/packs/all"
)

// TestExamplesMatchOnlyTheirStep checks every example against every
// step of every pack: it must match its own step and no other.
func TestExamplesMatchOnlyTheirStep(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	m := e.Manifests()["kafka"]
	if len(m.Steps) == 0 {
		t.Fatal("the kafka pack is not registered")
	}
	for _, s := range m.Steps {
		if s.Doc == "" || len(s.Examples) == 0 {
			t.Errorf("%s needs a Doc and Examples", s.ID)
		}
		for _, ex := range s.Examples {
			kw, text, _ := strings.Cut(ex, " ")
			if kw != s.Keyword {
				t.Errorf("%s: example %q should start with %s", s.ID, ex, s.Keyword)
			}
			ms := e.Registry.Match(text)
			if len(ms) != 1 || ms[0].Def().Step.ID != s.ID {
				var ids []string
				for _, x := range ms {
					ids = append(ids, x.Def().Step.ID)
				}
				t.Errorf("%s: example %q matches %v", s.ID, ex, ids)
			}
		}
	}
}
