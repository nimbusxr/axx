package lint

import (
	"fmt"
	"strings"
	"testing"

	messages "github.com/cucumber/messages/go/v34"

	"github.com/nimbusxr/axx/internal/feature"
)

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

  Scenario: what an earlier round did, as Given steps
    Given a POST request to /api/parcels
    And the request is executed
    When a 2nd ordered DELETE request to /api/parcels/PX-REG-1
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 204

  Scenario: register, then cancel
    Given a POST request to /api/parcels
    When the request is executed
    Then the response payload property status is 'REGISTERED'
    When a 2nd ordered DELETE request to /api/parcels/PX-REG-1
    And the 2nd ordered request is executed
    Then the response payload property status is 'CANCELLED'

  Scenario Outline: register, look up, then cancel
    Given a POST request to /api/parcels
    When the request is executed
    Then the response status code is <status>
    When a 2nd ordered GET request to /api/parcels/PX-REG-2
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200
    When a 3rd ordered DELETE request to /api/parcels/PX-REG-2
    And the 3rd ordered request is executed
    Then the 3rd ordered response status code is 204

    Examples:
      | status |
      | 201    |
      | 200    |
`

// A When after a Then starts another behavior: the check warns once per
// scenario, at the step that starts its second round.
func TestCheckRounds(t *testing.T) {
	_, pickles, err := feature.ParseSource("rounds.feature", []byte(roundsFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	rr := CheckRounds(pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityWarning || f.Code != CodeSeveralRounds {
			t.Errorf("finding: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s | %s", f.Locations[0].Line, f.Locations[0].Text, f.Message))
	}
	want := []string{
		"29 When a 2nd ordered DELETE request to /api/parcels/PX-REG-1 | a When after a Then: this scenario checks 2 behaviors in turn (When … Then …, then When … Then … again), which is 2 scenarios. " +
			"Write one per behavior: Given the state it starts from (what an earlier round did), When the one thing someone does, Then what is true afterwards",
		"37 When a 2nd ordered GET request to /api/parcels/PX-REG-2 | a When after a Then: this scenario checks 3 behaviors in turn (When … Then …, then When … Then … again), which is 3 scenarios. " +
			"Write one per behavior: Given the state it starts from (what an earlier round did), When the one thing someone does, Then what is true afterwards",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if rr.Files != 1 || rr.Mode != ModeWarn || rr.ID != RoundsRuleID {
		t.Errorf("rule: files %d, mode %s, id %s", rr.Files, rr.Mode, rr.ID)
	}
}
