package mock

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/match"
)

// record is a findings record as the axx WireMock extension writes it.
func record(findings ...map[string]any) map[string]any {
	return map[string]any{
		"format": 1, "extension": "0.1.0", "spec": "/var/openapi/address-service.yaml", "mode": "report",
		"findings": findings,
	}
}

func securityMissing(level string) map[string]any {
	return map[string]any{
		"key": "validation.request.security.missing", "level": level, "side": "request",
		"message": "The request requires security parameters. None found.",
	}
}

// contractSetup is setup with a journal of calls the extension validated;
// every scenario it returns shares one run (suite).
func contractSetup(t *testing.T, journal []journaled) (*match.Registry, func() *core.Scenario, *core.Suite, string) {
	t.Helper()
	fw := &fakeWireMock{journal: journal}
	srv := httptest.NewServer(fw.handler())
	t.Cleanup(srv.Close)
	reg, _, _, _ := setup(t)
	suite := core.NewSuite(core.SuiteOptions{})
	if err := (pack{}).Init(context.Background(), suite); err != nil {
		t.Fatal(err)
	}
	newScenario := func() *core.Scenario {
		return core.NewScenario(context.Background(), core.ScenarioInfo{}, suite, nil)
	}
	return reg, newScenario, suite, srv.URL
}

func TestContractViolationFailsTheCheckingStep(t *testing.T) {
	reg, newScenario, _, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: record(securityMissing("ERROR"))},
	})
	sc := newScenario()
	if err := step(t, reg, sc, "the mocked addresses service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	err := step(t, reg, sc, "the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil)
	if err == nil || !core.IsAssertion(err) {
		t.Fatalf("want an assertion failure, got %v", err)
	}
	for _, want := range []string{
		"The GET /v1/postcodes/DE/10115 request named check broke the contract of the mocked addresses service (/var/openapi/address-service.yaml)",
		"validation.request.security.missing (request): The request requires security parameters",
		`"Given the OpenAPI validation levels for the mocked addresses service are:"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message lacks %q:\n%s", want, err)
		}
	}
}

func TestScenarioLevelsRelaxAFinding(t *testing.T) {
	reg, newScenario, _, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: record(securityMissing("ERROR"))},
	})
	sc := newScenario()
	for _, s := range []struct {
		text  string
		table [][]string
	}{
		{"the mocked addresses service with the following properties:", [][]string{{"url", url}}},
		{"the OpenAPI validation levels for the mocked addresses service are:", [][]string{{"validation.request", "WARN"}}},
		{"the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil},
	} {
		if err := step(t, reg, sc, s.text, s.table); err != nil {
			t.Fatalf("%s: %v", s.text, err)
		}
	}
	if err := step(t, reg, sc, "the OpenAPI validation levels for the mocked addresses service are:", [][]string{{"validation", "LOUD"}}); err == nil ||
		!strings.Contains(err.Error(), "supported levels") {
		t.Errorf("invalid level: %v", err)
	}
}

func TestScenarioLevelsOverrideRecordedLevels(t *testing.T) {
	reg, newScenario, _, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: record(securityMissing("IGNORE"))},
	})
	sc := newScenario()
	if err := step(t, reg, sc, "the mocked addresses service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	if err := step(t, reg, sc, "the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil); err != nil {
		t.Fatalf("a finding the stub ignores passes: %v", err)
	}
	if err := step(t, reg, sc, "the OpenAPI validation levels for the mocked addresses service are:", [][]string{{"validation.request.security", "ERROR"}}); err != nil {
		t.Fatal(err)
	}
	if err := step(t, reg, sc, "the mocked request named check was received exactly 1 time", nil); err == nil {
		t.Fatal("the scenario's ERROR level should fail the check")
	}
}

func TestFinishReportsOnlyUncheckedViolations(t *testing.T) {
	reg, newScenario, suite, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: record(securityMissing("ERROR"))},
		{Method: "GET", URL: "/v1/postcodes/DE/00012", Record: record(securityMissing("ERROR"))},
		{Method: "GET", URL: "/v1/postcodes/DE/20095", Record: record(securityMissing("WARN"))},
		{Method: "GET", URL: "/v1/postcodes/DE/50667", Record: record()},
		{Method: "GET", URL: "/health"},
	})
	sc := newScenario()
	if err := step(t, reg, sc, "the mocked addresses service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	// The scenario checks (and answers for) the first call.
	_ = step(t, reg, sc, "the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil)

	err := (pack{}).Finish(context.Background(), suite)
	if err == nil {
		t.Fatal("the unchecked violation must fail the run")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Calls to the mocked addresses service ("+url+") broke its contract, and no scenario checked them:") ||
		!strings.Contains(msg, "GET /v1/postcodes/DE/00012 (/var/openapi/address-service.yaml)") {
		t.Errorf("report:\n%s", msg)
	}
	for _, not := range []string{"/DE/10115", "/DE/20095", "/DE/50667", "/health"} {
		if strings.Contains(msg, not) {
			t.Errorf("report should not mention %s:\n%s", not, msg)
		}
	}
}

func TestFinishWithoutViolations(t *testing.T) {
	_, _, suite, _ := contractSetup(t, nil)
	if err := (pack{}).Finish(context.Background(), suite); err != nil {
		t.Fatalf("no mocked service used: %v", err)
	}
}

func TestUnknownFindingsFormat(t *testing.T) {
	rec := record(securityMissing("ERROR"))
	rec["format"] = 2
	rec["extension"] = "9.0.0"
	reg, newScenario, _, url := contractSetup(t, []journaled{{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: rec}})
	sc := newScenario()
	if err := step(t, reg, sc, "the mocked addresses service with the following properties:", [][]string{{"url", url}}); err != nil {
		t.Fatal(err)
	}
	err := step(t, reg, sc, "the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil)
	if err == nil || !strings.Contains(err.Error(), "records OpenAPI findings in format 2 (axx-wiremock 9.0.0), and this axx reads format 1") {
		t.Fatalf("format mismatch: %v", err)
	}
}

type logSink struct{ logs []string }

func (l *logSink) Log(_ *core.Scenario, msg string)              { l.logs = append(l.logs, msg) }
func (l *logSink) Attach(*core.Scenario, string, []byte, string) {}

func TestUnusedRelaxationIsWarnedAndNamedInTheRunReport(t *testing.T) {
	reg, _, suite, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/00012", Record: record(securityMissing("ERROR"))},
	})
	sink := &logSink{}
	sc := core.NewScenario(context.Background(), core.ScenarioInfo{Name: "Registration without the key", URI: "features/x.feature", Line: 7}, suite, sink)
	for _, s := range []struct {
		text  string
		table [][]string
	}{
		{"the mocked addresses service with the following properties:", [][]string{{"url", url}}},
		{"the OpenAPI validation levels for the mocked addresses service are:", [][]string{{"validation.request.security", "WARN"}}},
	} {
		if err := step(t, reg, sc, s.text, s.table); err != nil {
			t.Fatalf("%s: %v", s.text, err)
		}
	}
	if err := warnUnusedLevels(sc); err != nil {
		t.Fatal(err)
	}
	if len(sink.logs) != 1 || !strings.Contains(sink.logs[0], "the OpenAPI validation levels set for the mocked addresses service applied to no call") {
		t.Errorf("warning: %v", sink.logs)
	}

	err := (pack{}).Finish(context.Background(), suite)
	if err == nil || !strings.Contains(err.Error(),
		`relaxed to WARN in "Registration without the key" (features/x.feature:7), which does not check this call`) {
		t.Fatalf("the run report should name the relaxing scenario: %v", err)
	}
}

func TestNoWarningWhenTheRelaxationApplied(t *testing.T) {
	reg, newScenario, _, url := contractSetup(t, []journaled{
		{Method: "GET", URL: "/v1/postcodes/DE/10115", Record: record(securityMissing("ERROR"))},
	})
	sink := &logSink{}
	sc := newScenario()
	sc = core.NewScenario(context.Background(), core.ScenarioInfo{}, sc.Suite(), sink)
	for _, s := range []struct {
		text  string
		table [][]string
	}{
		{"the mocked addresses service with the following properties:", [][]string{{"url", url}}},
		{"the OpenAPI validation levels for the mocked addresses service are:", [][]string{{"validation.request.security", "WARN"}}},
		{"the mocked GET request to /v1/postcodes/DE/10115 named check was received by addresses", nil},
	} {
		if err := step(t, reg, sc, s.text, s.table); err != nil {
			t.Fatalf("%s: %v", s.text, err)
		}
	}
	sink.logs = nil
	_ = warnUnusedLevels(sc)
	if len(sink.logs) != 0 {
		t.Errorf("no warning expected: %v", sink.logs)
	}
}
