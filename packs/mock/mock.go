// Package mock verifies requests received by WireMock mocks through the
// WireMock admin API. Stubs themselves come from WireMock mapping files
// mounted into the WireMock container; axx only verifies.
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/oaslevel"
)

// Service is a mocked (WireMock) service registered in a scenario.
type Service struct {
	Name string
	URL  string
	c    *client
	// requests are named request patterns. Header steps add constraints to
	// the stored pattern, and those constraints carry into later checks of the
	// same name (deliberately).
	requests map[string]*pattern
	// levels relax, for this scenario, OpenAPI findings on calls to this
	// service (the dependency's contract; see contract.go).
	levels oaslevel.Levels
	// checked counts the validated calls this scenario's mock steps checked.
	checked int
}

type ScenarioContext struct {
	services *core.Services[*Service]
	last     *verification
}

type verification struct {
	service string
	name    string
	pattern *pattern
	want    string
	got     int
}

var stateKey = core.NewStateKey("mock", func(*core.Scenario) *ScenarioContext {
	return &ScenarioContext{services: core.NewServices[*Service]("Mocked service", "")}
}, nil)

// describe summarizes the last verification for failure reports.
func (s *ScenarioContext) describe() any {
	if s.last == nil {
		return nil
	}
	return map[string]any{
		"service": s.last.service, "request": s.last.name, "expected": s.last.want,
		"received": s.last.got, "pattern": mustRaw(s.last.pattern),
	}
}

func mustRaw(p *pattern) any {
	if p == nil {
		return nil
	}
	return json.RawMessage(mustJSON(p))
}

// Pack returns the mock pack.
func Pack() core.Pack { return pack{} }

type pack struct{}

var (
	_ core.Initializer = pack{}
	_ core.Finisher    = pack{}
)

// Init notes when the run starts: contract findings are read from then on.
func (pack) Init(_ context.Context, s *core.Suite) error {
	runContracts(s)
	return nil
}

// Finish reports contract violations on calls no scenario checked.
func (pack) Finish(ctx context.Context, s *core.Suite) error {
	return runContracts(s).finish(ctx)
}

var sharedHTTP = &http.Client{Timeout: 30 * time.Second}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name:      "mock",
		Namespace: "mock",
		Doc:       "Verify requests received by WireMock mocks (stubs are defined in WireMock mapping files).",
		Hooks: []core.Hook{{
			ID: "mock.openapi.levels.unused", Phase: core.AfterScenario,
			Run: warnUnusedLevels,
		}},
		Params: []core.ParamType{{
			Name:    "mockedService",
			Regexps: []string{`([^\s]+)`},
			Doc:     "The name of a mocked service registered in the scenario.",
			Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
				return stateKey.Of(sc).services.Get(name)
			},
		}},
		Steps: steps(),
	}
}

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "mock.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the mocked {word} service with the following properties:",
			Doc: "Register a WireMock server. The first mocked service registered in a scenario is the default one.\n\n" +
				"Properties: `url` (required; `${env:..}`/`${sys:..}` are expanded).",
			Examples: []string{"Given the mocked addresses service with the following properties:"},
			Run:      addService,
		},
		{
			ID: "mock.received", Keyword: "Then",
			Expr: "the mocked {word} request to {word} named {word} was received by {mockedService}",
			Doc: "Register a request pattern under a name (method + exact URL, including the query string) and verify " +
				"WireMock received it at least once. Later steps refer to the pattern by name.",
			Examples: []string{"Then the mocked GET request to /v1/postcodes/DE/10115 named postcode-check was received by addresses"},
			Run:      received,
		},
		{
			ID: "mock.openapi.levels", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the OpenAPI validation levels for the mocked {mockedService} service are:",
			Doc: "Relax, for this scenario, the mocked service's OpenAPI contract: findings that would fail the checking mock step " +
				"are reported at the level you set instead (`key | level` rows; WARN logs them, INFO and IGNORE drop them). A key also " +
				"covers the keys below it: `validation.response.body` covers `validation.response.body.schema.required`. It applies to the " +
				"calls this scenario's mock steps check; a stub that is off-contract on purpose is better relaxed in its own metadata " +
				"(`openApiValidationLevels`), which applies wherever it answers. This is the dependency's contract: your own service's " +
				"is relaxed with `the OpenAPI validation levels are:`.",
			Examples: []string{"Given the OpenAPI validation levels for the mocked addresses service are:"},
			Run:      setLevels,
		},
		countStep("mock.count.exactly", "exactly", func(got, n int) bool { return got == n }),
		countStep("mock.count.atLeast", "at least", func(got, n int) bool { return got >= n }),
		countStep("mock.count.atMost", "at most", func(got, n int) bool { return got <= n }),
		{
			ID: "mock.notReceived", Keyword: "Then",
			Expr:     "the mocked {word} request to {word} named {word}[[ on {mockedService}]] was not received",
			Doc:      "Register a request pattern under a name and verify WireMock received no matching request.",
			Examples: []string{"Then the mocked GET request to /v1/postcodes/DE/12489 named skipped-check was not received"},
			Run:      notReceived,
		},
		{
			ID: "mock.header.is", Keyword: "Then",
			Expr:     "the header {word} for mocked request named {word}[[ on {mockedService}]] is {string}",
			Doc:      "Verify the named request was received with a header equal to the value. The constraint is added to the named pattern.",
			Examples: []string{"Then the header X-Api-Key for mocked request named postcode-check is 'example-address-key'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{EqualTo: a.String(3)})
			},
		},
		{
			ID: "mock.headers.are", Keyword: "Then", Arg: core.ArgTable,
			Expr:     "the headers for mocked request named {word} on {mockedService} are:",
			Doc:      "Verify the named request was received with every header in the table (name | value).",
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses are:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headersCheck(sc, a, func(v string) headerMatcher { return headerMatcher{EqualTo: v} })
			},
		},
		{
			ID: "mock.header.matches", Keyword: "Then",
			Expr:     "the header {word} for mocked request named {word} on {mockedService} matches {pattern}",
			Doc:      "Verify the named request was received with a header matching the regular expression (evaluated by WireMock, full match).",
			Examples: []string{"Then the header Accept for mocked request named postcode-check on addresses matches ^application/json$"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{Matches: a.String(3)})
			},
		},
		{
			ID: "mock.headers.match", Keyword: "Then", Arg: core.ArgTable,
			Expr:     "the headers for mocked request named {word} on {mockedService} match:",
			Doc:      "Verify the named request was received with headers matching each regular expression in the table (name | pattern).",
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses match:"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headersCheck(sc, a, func(v string) headerMatcher { return headerMatcher{Matches: v} })
			},
		},
		{
			ID: "mock.header.missing", Keyword: "Then",
			Expr:     "the header {word} for mocked request named {word} on {mockedService} is missing",
			Doc:      "Verify the named request was received without the header.",
			Examples: []string{"Then the header Authorization for mocked request named postcode-check on addresses is missing"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{Absent: true})
			},
		},
		{
			ID: "mock.headers.missing", Keyword: "Then", Arg: core.ArgTable,
			Expr:     "the headers for mocked request named {word} on {mockedService} are missing:",
			Doc:      "Verify the named request was received without any of the headers listed (one per row).",
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses are missing:"},
			Run:      headersMissing,
		},
	}
}

func countStep(id, words string, ok func(got, n int) bool) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then",
		Expr:     "the mocked request named {word}[[ on {mockedService}]] was received " + words + " {int} time(s)",
		Doc:      fmt.Sprintf("Verify the named request pattern was received %s the given number of times.", words),
		Examples: []string{fmt.Sprintf("Then the mocked request named postcode-check was received %s 1 time", words)},
		Run: func(sc *core.Scenario, a core.Args) error {
			svc, err := service(sc, a, 1)
			if err != nil {
				return err
			}
			name := a.String(0)
			p, err := named(svc, name)
			if err != nil {
				return err
			}
			n := a.Int(2)
			return verify(sc, svc, name, p, fmt.Sprintf("%s %d", words, n), func(got int) bool { return ok(got, n) })
		},
	}
}

func addService(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	props := map[string]string{}
	for _, p := range pairs {
		props[p.Key] = p.Value
	}
	raw, ok := props["url"]
	if !ok {
		return fmt.Errorf(`Property "url" is required`) //nolint:staticcheck // user-facing message
	}
	u := sc.Suite().Interpolate(raw)
	svc, err := NewService(a.String(0), u)
	if err != nil {
		return err
	}
	st := Context(sc)
	sc.Describe("mock", st.describe)
	if err := st.AddService(svc); err != nil {
		return err
	}
	runContracts(sc.Suite()).use(svc)
	return nil
}

func setLevels(sc *core.Scenario, a core.Args) error {
	svc := a.Value(0).(*Service)
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	m := map[string]string{}
	for _, p := range pairs {
		m[p.Key] = p.Value
	}
	lv, err := oaslevel.ParseMap(m)
	if err != nil {
		return err
	}
	svc.levels = svc.levels.Merge(lv)
	runContracts(sc.Suite()).relax(svc, lv, sc.ScenarioInfo)
	return nil
}

// warnUnusedLevels warns when a scenario relaxed a mocked service's contract
// but checked no call to it: the relaxation then applied to nothing, and the
// end-of-run check still reports the calls it meant to relax.
func warnUnusedLevels(sc *core.Scenario) error {
	for _, svc := range Context(sc).Services() {
		if len(svc.levels) > 0 && svc.checked == 0 {
			sc.Log("warning: the OpenAPI validation levels set for the mocked %s service applied to no call: no mock step "+
				"in this scenario checked a call to it. Check the call with a mock step (the mocked ... request ... was received by %s), "+
				"or relax the stub in its metadata (openApiValidationLevels).", svc.Name, svc.Name)
		}
	}
	return nil
}

func received(sc *core.Scenario, a core.Args) error {
	method, path, name := a.String(0), a.String(1), a.String(2)
	svc := a.Value(3).(*Service)
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD":
	default:
		return fmt.Errorf("Unsupported method: %s", method) //nolint:staticcheck // user-facing message
	}
	p := &pattern{Method: method, URL: path}
	svc.requests[name] = p
	return verify(sc, svc, name, p, "at least 1", func(got int) bool { return got >= 1 })
}

func notReceived(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 3)
	if err != nil {
		return err
	}
	method, path, name := a.String(0), a.String(1), a.String(2)
	p := &pattern{Method: method, URL: path}
	svc.requests[name] = p
	return verify(sc, svc, name, p, "exactly 0", func(got int) bool { return got == 0 })
}

func headerCheck(sc *core.Scenario, a core.Args, header, name string, m headerMatcher) error {
	svc, err := service(sc, a, 2)
	if err != nil {
		return err
	}
	p, err := named(svc, name)
	if err != nil {
		return err
	}
	p.withHeader(header, m)
	return verify(sc, svc, name, p, "at least 1", func(got int) bool { return got >= 1 })
}

func headersCheck(sc *core.Scenario, a core.Args, mk func(string) headerMatcher) error {
	name := a.String(0)
	svc := a.Value(1).(*Service)
	p, err := named(svc, name)
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	for _, kv := range pairs {
		p.withHeader(kv.Key, mk(kv.Value))
	}
	return verify(sc, svc, name, p, "at least 1", func(got int) bool { return got >= 1 })
}

func headersMissing(sc *core.Scenario, a core.Args) error {
	name := a.String(0)
	svc := a.Value(1).(*Service)
	p, err := named(svc, name)
	if err != nil {
		return err
	}
	for _, row := range a.Table.Rows {
		if len(row) != 1 {
			return fmt.Errorf("the headers table must have exactly one column (header names), got %d", len(row))
		}
		p.withHeader(row[0], headerMatcher{Absent: true})
	}
	return verify(sc, svc, name, p, "at least 1", func(got int) bool { return got >= 1 })
}

// service returns the named service from argument i, or the default.
func service(sc *core.Scenario, a core.Args, i int) (*Service, error) {
	if a.Present(i) {
		return a.Value(i).(*Service), nil
	}
	return stateKey.Of(sc).services.Default()
}

func named(svc *Service, name string) (*pattern, error) {
	p, ok := svc.requests[name]
	if !ok {
		return nil, fmt.Errorf("no mocked request named %q on %s; register it first, e.g. "+
			"\"the mocked GET request to /path named %s was received by %s\"", name, svc.Name, name, svc.Name)
	}
	return p, nil
}

func verify(sc *core.Scenario, svc *Service, name string, p *pattern, want string, ok func(int) bool) error {
	got, err := svc.c.count(sc.Context(), p)
	if err != nil {
		return err
	}
	st := stateKey.Of(sc)
	st.last = &verification{service: svc.Name, name: name, pattern: p, want: want, got: got}
	if ok(got) {
		if got == 0 {
			return nil
		}
		return checkContract(sc, svc, name, p)
	}
	msg := fmt.Sprintf("Expected %s request(s) matching %s on %s but received %d.\nPattern:\n%s",
		want, name, svc.Name, got, p.describe())
	if misses, err := svc.c.nearMisses(sc.Context(), p); err == nil && len(misses) > 0 {
		var b strings.Builder
		b.WriteString("\nClosest requests received:")
		for i, m := range misses {
			if i == 3 {
				break
			}
			fmt.Fprintf(&b, "\n  %s %s", m.Request.Method, m.Request.URL)
		}
		msg += b.String()
	}
	return core.Fail(msg, want, got)
}
