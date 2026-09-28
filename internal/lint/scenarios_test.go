package lint

import (
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/engine"
	"github.com/nimbusxr/axx/internal/feature"
	"github.com/nimbusxr/axx/packs/all"
)

const checksFeature = `Feature: What scenarios check

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |
    And the mocked courier service with the following properties:
      | url | http://localhost:8082 |

  Scenario: only a success status
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 201

  Scenario: a success status and what the request did
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 201
    And the response payload property status is 'REGISTERED'

  Scenario: a refusal is its own proof
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 400

  Scenario: only that nothing happened
    Given a DELETE request to /api/parcels/PX-REG-1602
    When the request is executed
    Then the mocked DELETE request to path /v1/collections/PX-REG-1602 named call-off was not received

  Scenario: nothing happened, after something did
    Given a DELETE request to /api/parcels/PX-REG-1602
    When the request is executed
    Then the response status code is 204
    And none of the mocked DELETE requests to path /v1/collections on courier have the query parameters:
      | reference | PX-REG-1602 |

  Scenario Outline: an outline is one scenario, whichever its examples
    Given a GET request to /api/parcels/<reference>
    When the request is executed
    Then the response status code is <status>

    Examples:
      | reference   | status |
      | PX-REG-1001 | 200    |
      | PX-REG-1002 | 200    |

  Scenario: a step no definition matches leaves the scenario out
    Given a GET request to /api/parcels
    When the request is sent somewhere unknown
    Then the response status code is 200
`

func TestScenarioHints(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("checks.feature", []byte(checksFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	got := ScenarioHints(e.Registry, pickles, Options{})
	want := []string{
		"2 scenarios check only a success status code, which says a request was accepted, not what it did: checks.feature:9, checks.feature:37 (check what it did too: a response property, a row, a message)",
		"1 scenario checks only that something did not happen, which also passes when the action never ran: checks.feature:25 (check something it did do too)",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("hints:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestScenarioHintsNameAFewScenarios(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("Feature: Many\n\n  Background:\n    Given the parcels service with the following properties:\n      | url | http://localhost:8400 |\n")
	for range 7 {
		b.WriteString("\n  Scenario: only a success status\n    Given a GET request to /api/parcels\n    When the request is executed\n    Then the response status code is 200\n")
	}
	_, pickles, err := feature.ParseSource("many.feature", []byte(b.String()), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	got := ScenarioHints(e.Registry, pickles, Options{})
	if len(got) != 1 || !strings.Contains(got[0], "7 scenarios check only a success status code") ||
		!strings.Contains(got[0], "many.feature:27 and 2 more (check") {
		t.Errorf("hints: %v", got)
	}
}
