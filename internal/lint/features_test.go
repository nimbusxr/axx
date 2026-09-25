package lint

import (
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

func TestOrdinalStepsMatchRegistry(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	for id, use := range ordinalSteps {
		d, ok := e.Registry.Def(id)
		if !ok {
			t.Errorf("%s: no such step", id)
			continue
		}
		if use.ordinal >= len(d.Names) || d.Names[use.ordinal] != "ordinal" {
			t.Errorf("%s: argument %d is not the ordinal: %v", id, use.ordinal, d.Names)
		}
		if use.service >= len(d.Names) || d.Names[use.service] != "dbService" {
			t.Errorf("%s: argument %d is not the service: %v", id, use.service, d.Names)
		}
	}
	// Every SQL step using a selection or trigger ordinal is covered.
	for _, d := range e.Registry.Defs() {
		expr := d.Step.Expr
		if d.Pack == "sql" && (strings.Contains(expr, "{ordinal}]] selection") || strings.Contains(expr, "{ordinal} ordered]]")) {
			if _, ok := ordinalSteps[d.Step.ID]; !ok {
				t.Errorf("%s (%s) is not in ordinalSteps", d.Step.ID, expr)
			}
		}
	}
}

const ordinalFeature = `Feature: ordinals

  Background:
    Given a selection of rows is retrieved from the t.bg table where:
      | a | b |

  Scenario: addressing and labelling selections
    Then the 2nd selection has 1 row
    And a selection of rows is retrieved from the t.x table where:
      | a | b |
    And the 2nd selection has 1 row
    And the 3rd selection has 1 row
    And a 4th selection of rows is retrieved from the t.x table where:
      | a | b |
    And a 2nd selection of rows is retrieved from the t.x table where:
      | a | b |
    And the 4th selection has 2 rows
    And the 1st row metadata property for the 5th selection json properties are:
      | a | b |

  Scenario: services cannot be told apart, so nothing is flagged
    Then a 2nd selection of rows is retrieved from the t.x table on db1 where:
      | a | b |
    And a 1st selection of rows is retrieved from the t.x table on db2 where:
      | a | b |
    And the 3rd selection on db2 has 1 row
    And within 5s a 4th selection of at least 1 row is retrieved from the t.x table where:
      | a | b |

  Scenario: registering a service resets what is known per service
    Then a 2nd selection of rows is retrieved from the t.x table where:
      | a | b |
    And a db2 database with the following properties:
      | url      | jdbc:postgresql://localhost/db |
      | user     | u                              |
      | password | p                              |
    And a 1st selection of rows is retrieved from the t.x table where:
      | a | b |

  Scenario Outline: triggers
    Given a <n> ordered before insert trigger on the t.x table will raise a 23505 exception where:
      | a | b |
    Then the 2nd ordered before insert trigger on the t.x table was raised 1 time

    Examples:
      | n   |
      | 1st |
      | 2nd |
`

func TestCheckFeatures(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("ordinals.feature", []byte(ordinalFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pickles {
		for _, ps := range p.Steps {
			if len(e.Registry.Match(ps.Text)) != 1 {
				t.Fatalf("fixture step does not match exactly one definition: %s", ps.Text)
			}
		}
	}
	rr := CheckFeatures(e.Registry, pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning {
			t.Errorf("feature findings are warnings: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s %s", f.Locations[0].Line, f.Code, f.Message))
	}
	want := []string{
		"8 AXX-E0830 this step uses the 2nd selection, but only 1 selection was retrieved before it in the scenario, so the step always fails",
		"12 AXX-E0830 this step uses the 3rd selection, but only 2 selections were retrieved before it in the scenario, so the step always fails",
		"13 AXX-E0831 this step is labelled the 4th selection, but only 2 selections were retrieved before it in the scenario, so it is at most the 3rd; " + labels("selection", "retrieved"),
		"15 AXX-E0831 this step is labelled the 2nd selection, but 3 selections of the default service were retrieved before it, so it is at least the 4th; " + labels("selection", "retrieved"),
		"18 AXX-E0830 this step uses the 5th selection, but only 4 selections were retrieved before it in the scenario, so the step always fails",
		"41 AXX-E0831 this step is labelled the 2nd trigger, but no triggers were created before it in the scenario, so it is the 1st; " + labels("trigger", "created"),
		"43 AXX-E0830 this step uses the 2nd trigger, but only 1 trigger was created before it in the scenario, so the step always fails",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rr.Findings[0].Locations[0].Text != "Then the 2nd selection has 1 row" {
		t.Errorf("location text: %q", rr.Findings[0].Locations[0].Text)
	}
}

func labels(list, verb string) string {
	return "ordinals in these steps are labels only: " + list + "s are numbered in the order they are " + verb
}

func TestOrdinalWords(t *testing.T) {
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 102: "102nd", 111: "111th"} {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %s, want %s", n, got, want)
		}
	}
}
