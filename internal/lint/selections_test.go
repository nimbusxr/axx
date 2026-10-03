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

func TestSelectionStepsMatchRegistry(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	for id, use := range selectionSteps {
		d, ok := e.Registry.Def(id)
		if !ok {
			t.Errorf("%s: no such step", id)
			continue
		}
		if use.ordinal >= len(d.Names) || d.Names[use.ordinal] != "ordinal" {
			t.Errorf("%s: argument %d is not the ordinal: %v", id, use.ordinal, d.Names)
		}
		if use.service >= len(d.Names) || (d.Names[use.service] != "dbService" && d.Names[use.service] != "mongoService") {
			t.Errorf("%s: argument %d is not the service: %v", id, use.service, d.Names)
		}
	}
	// Every SQL and MongoDB step with a selection ordinal is covered.
	for _, d := range e.Registry.Defs() {
		if (d.Pack == "sql" || d.Pack == "mongo") && strings.Contains(d.Step.Expr, "{ordinal}]] selection") {
			if _, ok := selectionSteps[d.Step.ID]; !ok {
				t.Errorf("%s (%s) is not in selectionSteps", d.Step.ID, d.Step.Expr)
			}
		}
	}
}

const selectionsFeature = `Feature: selections

  Background:
    Given a parcels-db database with the following properties:
      | url | postgres://localhost/parcels |

  Scenario: the recipient's selection is never checked
    Given a seeds/manifest.yaml db seed
    Then within 10s a selection of at least 1 row is retrieved from the parcels.parcels table where:
      | reference | PX-6401 |
    And the selection has 1 row
    And a selection of rows is retrieved from the parcels.parcels table where the recipient jsonb column contains:
      | postcode | SW1A1AA |
    And the selection has 1 row

  Scenario: the document read after the late scan is never checked
    Given a tracking-db mongo database with the following properties:
      | url | mongodb://localhost/tracking |
    And within 10s a selection of at least 1 document is retrieved from the tracking collection on tracking-db where:
      | _id | PX-TRK-6201 |
    When a seeds/late-scan.json MongoDB seed for tracking-db
    Then within 10s a selection of at least 1 document is retrieved from the tracking collection on tracking-db where:
      | scanCount | 2 |
    And the 1st document for the selection on tracking-db properties are:
      | status | IN_TRANSIT |

  Scenario: every selection is checked, the first by the step without an ordinal
    When within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id | ML-1 |
    And within 10s a 2nd selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id | ML-2 |
    Then the selection has 1 row
    And the 2nd selection has 1 row

  Scenario: selections of two services are left alone
    Given a billing-db database with the following properties:
      | url | postgres://localhost/billing |
    When a selection of rows is retrieved from the parcels.parcels table on parcels-db where:
      | reference | PX-1 |
    And a selection of rows is retrieved from the billing.invoices table on billing-db where:
      | reference | PX-1 |
    Then the selection on parcels-db has 1 row
`

// The stale selections of two eval suites that missed their planted bugs
// are found; a scenario that checks every selection, or whose selections
// belong to two services, is not flagged.
func TestCheckSelections(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("selections.feature", []byte(selectionsFeature), messages.UUID{}.NewId)
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
	rr := CheckSelections(e.Registry, pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning || f.Code != CodeStaleSelection {
			t.Errorf("finding: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s", f.Locations[0].Line, f.Message))
	}
	want := []string{
		"14 this step checks the 1st selection (line 9), not the 2nd (line 12), which no step checks: a step without an ordinal means the first; to check the 2nd, name it: `the 2nd selection has 1 row`",
		"24 this step checks the 1st selection (line 19), not the 2nd (line 22), which no step checks: a step without an ordinal means the first; to check the 2nd, name it: `the 1st document for the 2nd selection on tracking-db properties are:`",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rr.ID != SelectionsRuleID || rr.Files != 1 {
		t.Errorf("rule %s, %d files", rr.ID, rr.Files)
	}
}
