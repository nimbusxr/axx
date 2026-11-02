package packref

import (
	"slices"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/internal/match"
	"github.com/nimbusxr/axx/internal/packset"
)

// Every pack axx publishes has its reference, and nothing else does.
func TestEveryPublishedPack(t *testing.T) {
	if got, want := Names(), packset.Names(); !slices.Equal(sorted(got), want) {
		t.Errorf("the reference has %v, the catalog %v: run go generate ./internal/packref", got, want)
	}
	for _, p := range packset.Catalog {
		r, ok := Lookup(p.Name)
		if !ok {
			continue
		}
		if !slices.Equal(r.Requires, p.Requires) {
			t.Errorf("%s requires %v in the reference, %v in the catalog", p.Name, r.Requires, p.Requires)
		}
	}
}

// The reference's steps match as the packs' own do, without their code:
// every example of every step matches it, in a registry of its pack alone.
func TestStepsMatchFromTheReference(t *testing.T) {
	for _, p := range Packs() {
		r, err := Registry(p.Name)
		if err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
		for _, s := range p.Steps {
			for _, ex := range s.Examples {
				line := firstLine(ex)
				ms := r.Match(line)
				if !slices.ContainsFunc(ms, func(m match.Match) bool { return m.Def().Step.ID == s.ID }) {
					t.Errorf("%s: example %q of %s does not match it", p.Name, line, s.ID)
				}
			}
		}
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

// firstLine is an example's step line, without its keyword, table or doc
// string.
func firstLine(ex string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(ex), "\n")
	for _, kw := range []string{"Given ", "When ", "Then ", "And ", "But ", "* "} {
		if rest, ok := strings.CutPrefix(line, kw); ok {
			return strings.TrimSpace(rest)
		}
	}
	return strings.TrimSpace(line)
}
