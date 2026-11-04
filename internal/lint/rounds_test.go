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

// A When after a Then starts another behavior: the check fails once per
// scenario, at the step that starts its second round.
func TestCheckRounds(t *testing.T) {
	_, pickles, err := feature.ParseSource("rounds.feature", []byte(roundsFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	rr := CheckRounds(pickles, "")
	var got []string
	for _, f := range rr.Findings {
		if f.Severity != SeverityError {
			t.Errorf("finding: %+v", f)
		}
		// A check of the starting state is no round, but it is a Then all the same.
		if f.Code == CodeSecondClause && f.Locations[0].Line == 16 {
			continue
		}
		if f.Code != CodeSeveralRounds {
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
	if rr.Files != 1 || rr.Mode != ModeError || rr.ID != RoundsRuleID {
		t.Errorf("rule: files %d, mode %s, id %s", rr.Files, rr.Mode, rr.ID)
	}
}

const clausesFeature = `Feature: Clauses

  Scenario: two Whens
    Given a GET request to /a
    When the request is executed
    When the request is executed
    Then the response status code is 200

  Scenario: two Thens
    Given a GET request to /b
    When the request is executed
    Then the response status code is 200
    Then the response status code is 200

  Scenario: Ands add to a When and a Then
    Given a GET request to /c
    When the request is executed
    And the request is executed
    Then the response status code is 200
    And the response status code is 200
    But the response status code is 200

  Scenario: a second round is reported once, as a round
    Given a GET request to /d
    When the request is executed
    Then the response status code is 200
    When the request is executed
    Then the response status code is 200

  Scenario Outline: an outline's examples report a line once
    Given a GET request to /<path>
    When the request is executed
    When the request is executed
    Then the response status code is 200

    Examples:
      | path |
      | e    |
      | f    |
`

func TestCheckRoundsFindsASecondWhenOrThen(t *testing.T) {
	_, pickles, err := feature.ParseSource("clauses.feature", []byte(clausesFeature), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range CheckRounds(pickles, "").Findings {
		if f.Severity != SeverityError {
			t.Errorf("finding: %+v", f)
		}
		got = append(got, fmt.Sprintf("%d %s %s", f.Locations[0].Line, f.Code, f.Locations[0].Text))
	}
	want := []string{
		"6 " + CodeSecondClause + " When the request is executed",
		"13 " + CodeSecondClause + " Then the response status code is 200",
		"27 " + CodeSeveralRounds + " When the request is executed",
		"33 " + CodeSecondClause + " When the request is executed",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The check goes by what a keyword is, not by its English word.
func TestCheckRoundsReadsAnyLanguage(t *testing.T) {
	src := "# language: de\nFunktionalität: Runden\n\n  Szenario: zwei\n    Wenn the request is executed\n    Wenn the request is executed\n"
	_, pickles, err := feature.ParseSource("de.feature", []byte(src), messages.UUID{}.NewId)
	if err != nil {
		t.Fatal(err)
	}
	if fs := CheckRounds(pickles, "").Findings; len(fs) != 1 || fs[0].Code != CodeSecondClause {
		t.Errorf("findings: %+v", fs)
	}
}
