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

  Scenario: a refusal checked only by its status
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 400

  Scenario: a refusal and why
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 400
    And the response body contains 'weightGrams'

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
		"2 scenarios check only a success status (a 2xx response, a command's exit code 0), which says it was accepted, not what it did: checks.feature:9, checks.feature:43 (check what it did too: a response property, a row, a message, the output)",
		"1 scenario checks only status codes, a refusal among them: a request refused for another reason (a missing field) passes as well as one refused by the rule meant: checks.feature:20 (check what the response says too, like the problem detail naming the field)",
		"1 scenario checks only that something did not happen, which also passes when the action never ran: checks.feature:31 (check something it did do too)",
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
	if len(got) != 1 || !strings.Contains(got[0], "7 scenarios check only a success status") ||
		!strings.Contains(got[0], "many.feature:27 and 2 more (check") {
		t.Errorf("hints: %v", got)
	}
}

const roundsFeature = `Feature: Rounds

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  Scenario: one round
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is 201
    And the response payload property status is 'REGISTERED'

  Scenario: a check of the starting state is no round
    Then the response payload property status is 'REGISTERED'
    When the request is executed
    Then the response payload property status is 'CANCELLED'

  Scenario: register, then cancel
    Given a POST request to /api/parcels
    When the request is executed
    Then the response payload property status is 'REGISTERED'
    When a 2nd ordered DELETE request to /api/parcels/PX-REG-1
    And the 2nd ordered request is executed
    Then the response payload property status is 'CANCELLED'
`

// A scenario that checks several things in turn (When … Then … When …
// Then …) is named: each acceptance criterion reads best as its own.
func TestScenarioHintsNameScenariosOfSeveralRounds(t *testing.T) {
	e, err := engine.New(engine.Options{Packs: engine.Ordered(all.Packs())})
	if err != nil {
		t.Fatal(err)
	}
	_, pickles, err := feature.ParseSource("rounds.feature", []byte(roundsFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	got := ScenarioHints(e.Registry, pickles, Options{})
	want := "1 scenario checks several things in turn (When … Then …, then When … Then … again): rounds.feature:18 (each acceptance criterion reads best as a scenario of its own)"
	if len(got) != 1 || got[0] != want {
		t.Errorf("hints:\n%s\nwant:\n%s", strings.Join(got, "\n"), want)
	}
}
