package lint

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v35"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/config"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
)

// A pack that gives a meaning to the tags of a depot (@depot:LEJ), and checks that a feature
// names a depot it knows: its settings say which.
type depotsPack struct{}

func (depotsPack) Manifest() core.Manifest {
	return core.Manifest{Name: "depots", FeatureChecks: []core.FeatureCheck{{
		ID: "depot-tags", Name: "Depot tags", Description: "A depot's tag names a depot the project has.",
		Run: func(call *core.FeatureCheckCall) []core.FeatureFinding {
			var settings struct {
				Depots []string `json:"depots"`
			}
			if err := call.Settings(&settings); err != nil {
				return nil
			}
			var out []core.FeatureFinding
			for _, sc := range call.Scenarios {
				for _, tag := range sc.Tags {
					depot, ok := strings.CutPrefix(tag, "@depot:")
					if !ok || strings.Contains(strings.Join(settings.Depots, " "), depot) {
						continue
					}
					out = append(out, core.FeatureFinding{URI: sc.URI, Line: sc.Line, Tag: tag, Message: "no depot " + depot + ": the project's depots are " + strings.Join(settings.Depots, ", ")})
				}
				if len(sc.Tags) == 0 {
					out = append(out, core.FeatureFinding{URI: sc.URI, Line: sc.Line, Message: "the scenario names no depot"})
				}
			}
			return out
		},
	}}}
}

const depotsFeature = `@depot:LEJ
Feature: sorting

  @depot:HAM
  Scenario: a parcel is sorted in Hamburg
    Given a parcel

  Rule: at night
    @depot:MUC @smoke
    Scenario: a parcel is sorted at night
      Given a parcel

    @depot:FRA
    Scenario Outline: a parcel of a size is sorted
      Given a parcel of <size>

      @depot:CGN
      Examples:
        | size  |
        | small |
        | large |
`

// A pack's check of the features is one of lint's rules: its findings are warnings, each where
// the tag it is about is written (a tag of an Examples table once, for all of its rows), and
// the check is given its pack's settings.
func TestAPacksCheckIsOneOfLints(t *testing.T) {
	e, err := engine.New(engine.Options{
		Config: &config.Config{Packs: map[string]json.RawMessage{"depots": json.RawMessage(`{"depots": ["LEJ", "HAM", "FRA"]}`)}},
		Packs:  []engine.NamedPack{{Name: "depots", Pack: depotsPack{}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("sorting.feature", []byte(depotsFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	results := FeatureChecks(e.Registry, pickles, "")
	rr := results[len(results)-1]
	if rr.ID != "depot-tags" || rr.Name != "Depot tags" || rr.Mode != ModeWarn || !rr.OK() || rr.Source != "the depots pack" {
		t.Fatalf("the pack's rule: %+v", rr)
	}
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning || f.Code != CodePackCheck {
			t.Errorf("finding: %+v", f)
		}
		l := f.Locations[0]
		got = append(got, fmt.Sprintf("%d:%d %s: %s", l.Line, l.Column, l.Text, f.Message))
	}
	want := []string{
		"9:5 @depot:MUC: no depot MUC: the project's depots are LEJ, HAM, FRA",
		"17:7 @depot:CGN: no depot CGN: the project's depots are LEJ, HAM, FRA",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// A finding that is about no tag is reported at its scenario, and a project whose packs check
// nothing has no rule of theirs.
func TestAPacksFindingWithoutATagIsItsScenarios(t *testing.T) {
	e, err := engine.New(engine.Options{Config: &config.Config{}, Packs: []engine.NamedPack{{Name: "depots", Pack: depotsPack{}}}})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("sorting.feature", []byte("Feature: sorting\n\n  Scenario: a parcel is sorted\n    Given a parcel\n"), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	rules := CheckPacks(e.Registry, pickles, "")
	if len(rules) != 1 || len(rules[0].Findings) != 1 {
		t.Fatalf("rules: %+v", rules)
	}
	if l := rules[0].Findings[0].Locations[0]; l.Line != 3 || l.Text != "Scenario: a parcel is sorted" {
		t.Errorf("the finding's place: %+v", l)
	}
	plain, err := engine.New(engine.Options{Config: &config.Config{}, Packs: []engine.NamedPack{}})
	if err != nil {
		t.Fatal(err)
	}
	if rules := CheckPacks(plain.Registry, pickles, ""); len(rules) != 0 {
		t.Errorf("a project whose packs check nothing: %+v", rules)
	}
}
