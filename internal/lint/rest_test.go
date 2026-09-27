package lint

import (
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

const restFeature = `Feature: REST requests

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  Scenario: requests out of order
    Given a POST request to /api/parcels
    And a 3rd ordered GET request to /api/parcels/PX-1001
    And a GET request to /api/parcels/PX-1002
    When the request is executed
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200

  Scenario: requests of a named service
    Given the journal service with the following properties:
      | url | http://localhost:8410 |
    And a GET request to /entries on journal
    And a 2nd ordered GET request to /entries?page=2 on journal
    And a 2nd ordered GET request to /api/parcels
    When the 2nd ordered request is executed on journal
    Then the response status code is 200

  Scenario: a project's step may add requests, so what follows is not checked
    Given a GET request to /api/parcels
    And the shop's signed webhook request to /webhooks/stripe
    When the 2nd ordered request is executed
`

// A project's pack whose step adds a request through the rest pack's
// context, out of the lint's sight.
type webhookPack struct{}

func (webhookPack) Manifest() core.Manifest {
	return core.Manifest{Name: "webhooks", Steps: []core.StepDef{{
		ID: "webhooks.signed", Keyword: "Given", Expr: "the shop's signed webhook request to {word}",
		Run: func(*core.Scenario, core.Args) error { return nil },
	}}}
}

func TestCheckRESTRequests(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: append(engine.Ordered(all.Packs()), engine.NamedPack{Name: "webhooks", Pack: webhookPack{}})})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("requests.feature", []byte(restFeature), messages.UUID{}.NewId)
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
	rr := CheckRESTRequests(e.Registry, pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning {
			t.Errorf("feature findings are warnings: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s %s", f.Locations[0].Line, f.Code, f.Message))
	}
	want := []string{
		"9 AXX-E0832 this step adds the 3rd request of service parcels, but only 1 request was added before it, so it fails; add the 2nd first: requests are numbered in the order they are added",
		"10 AXX-E0833 this step adds the 1st request of service parcels, which an earlier step added, so it fails; add another with `a 2nd ordered ... request`",
		"12 AXX-E0832 this step uses the 2nd request of service parcels, but only 1 request was added before it, so it always fails",
		"13 AXX-E0832 this step uses the 2nd request of service parcels, but only 1 request was added before it, so it always fails",
		"20 AXX-E0832 this step adds the 2nd request of service parcels, but no request was added before it, so it fails; add the 1st first: requests are numbered in the order they are added",
		"22 AXX-E0832 this step uses the 1st request of service parcels, but no request was added before it, so it always fails",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rr.ID != RESTRuleID || rr.Files != 1 {
		t.Errorf("rule %s, %d files", rr.ID, rr.Files)
	}
}

// Without a registration in sight, the default service is unknown: nothing
// is flagged.
func TestRESTRequestsOfAnUnseenDefaultServiceAreNotChecked(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	src := "Feature: f\n\n  Scenario: s\n    Given a 3rd ordered GET request to /api/parcels\n    When the 2nd ordered request is executed\n"
	_, pickles, err := feature.ParseSource("f.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	if rr := CheckRESTRequests(e.Registry, pickles, ""); len(rr.Findings) != 0 {
		t.Errorf("findings: %+v", rr.Findings)
	}
}
