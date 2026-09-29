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
		Doc: "Verify the requests WireMock mocks received. Stubs are defined in WireMock mapping files; axx only verifies.\n\n" +
			"With the axx WireMock image (`ghcr.io/nimbusxr/axx-wiremock`), every call to a mock is checked against the mocked " +
			"service's OpenAPI contract. A step that checks a call that broke the contract fails; a call that broke it, and that " +
			"no step checks, fails the run after the scenarios.\n\n" +
			"The image also mocks AI models, in the format of each request (OpenAI's API and the servers that speak it, Anthropic, " +
			"Gemini, Bedrock, Ollama): its stubs answer with the `model-request` matcher and the `model-answer` transformer, and " +
			"the `the mocked ... model ...` steps check what the service asked the model: the texts, the tools and the schema.",
		Hooks: []core.Hook{{
			ID: "mock.openapi.levels.unused", Phase: core.AfterScenario,
			Run: warnUnusedLevels,
		}},
		Params: []core.ParamType{{
			Name:     "mockedService",
			Regexps:  []string{`([^\s]+)`},
			Doc:      "the name of a mocked service the scenario registered",
			Examples: []string{"addresses"},
			Transform: func(sc *core.Scenario, name string, _ []*string) (any, error) {
				return stateKey.Of(sc).services.Get(name)
			},
		}},
		Steps: steps(),
	}
}

func steps() []core.StepDef {
	return append([]core.StepDef{
		{
			ID: "mock.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the mocked {word} service with the following properties:",
			Doc: "Register a WireMock server, to check the requests it received. The first mocked service registered in a " +
				"scenario is the default one.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "the WireMock server's URL, with its admin API at `/__admin` below it; it can use `${env:…}` " +
						"and `${sys:…}`", Required: true},
				},
				Note: "Any other property fails the step.",
			},
			Examples: []string{"Given the mocked addresses service with the following properties:\n" +
				"  | url | http://localhost:8081 |"},
			Run: addService,
		},
		{
			ID: "mock.received", Keyword: "Then",
			Expr: "the mocked {word} request to {word} named {word} was received by {mockedService}",
			Doc: "Check that the mocked service received a request at least once, and name it for the steps that follow.\n\n" +
				"- The request is its method and its exact URL, query string included.\n" +
				"- The method is `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS` or `HEAD`, in capitals.\n" +
				"- With the axx WireMock image, a call that broke the service's OpenAPI contract fails the step.",
			Examples: []string{"Then the mocked GET request to /v1/postcodes/DE/10115 named postcode-check was received by addresses"},
			Run:      received,
		},
		{
			ID: "mock.received.path", Keyword: "Then",
			Expr: "the mocked {word} request to path {word} named {word} was received by {mockedService}",
			Doc: "Check that the mocked service received a request to a path at least once, whatever its query string, and name it " +
				"for the steps that follow.\n\n" +
				"- For requests whose query changes from call to call, such as a request ID: check the query parameters that matter " +
				"with `the query parameters for mocked request named ... are:`.\n" +
				"- The method is `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS` or `HEAD`, in capitals.\n" +
				"- With the axx WireMock image, a call that broke the service's OpenAPI contract fails the step.",
			Examples: []string{"Then the mocked POST request to path /v1/collections named collection was received by courier"},
			Run:      receivedPath,
		},
		{
			ID: "mock.openapi.levels", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the OpenAPI validation levels for the mocked {mockedService} service are:",
			Doc: "Relax the mocked service's OpenAPI contract for this scenario: a finding that would fail the mock step " +
				"that checks the call is reported at the level its row sets instead.\n\n" +
				"- `WARN` logs the finding; `INFO` and `IGNORE` drop it.\n" +
				"- A key also covers the keys below it: `validation.response.body` covers `validation.response.body.schema.required`.\n" +
				"- It applies to the calls this scenario's mock steps check. A scenario whose mock steps check no call to the " +
				"service logs a warning.\n" +
				"- A stub that is off-contract on purpose is better relaxed in its own metadata (`openApiValidationLevels`), " +
				"which applies wherever it answers.\n" +
				"- This is the dependency's contract: your own service's is relaxed with `the OpenAPI validation levels are:`.",
			Table: &core.TableDoc{
				Columns: []string{"validation key", "level"},
				Note: "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
					"The key must be one the axx WireMock extension reports, or a prefix of such keys, like `validation.response.body`: " +
					"any other fails the step, which names the closest keys.",
			},
			Examples: []string{"Given the OpenAPI validation levels for the mocked addresses service are:\n" +
				"  | validation.response.body.schema.additionalProperties | WARN |"},
			Run: setLevels,
		},
		countStep("mock.count.exactly", "exactly", func(got, n int) bool { return got == n }),
		countStep("mock.count.atLeast", "at least", func(got, n int) bool { return got >= n }),
		countStep("mock.count.atMost", "at most", func(got, n int) bool { return got <= n }),
		{
			ID: "mock.notReceived", Keyword: "Then", Absence: true,
			Expr: "the mocked {word} request to {word} named {word}[[ on {mockedService}]] was not received",
			Doc: "Check that the mocked service received no request with that method and exact URL, and name it for the " +
				"steps that follow.",
			Examples: []string{"Then the mocked GET request to /v1/postcodes/DE/12489 named skipped-check was not received"},
			Run:      notReceived,
		},
		{
			ID: "mock.notReceived.path", Keyword: "Then", Absence: true,
			Expr: "the mocked {word} request to path {word} named {word}[[ on {mockedService}]] was not received",
			Doc: "Check that the mocked service received no request with that method to a path, whatever its query string, and " +
				"name it for the steps that follow.\n\n" +
				"- Every request the mocked service received counts, other scenarios' too: the path must be the scenario's own, like " +
				"one with its parcel's reference. For a path every scenario calls, check that none of its requests has the scenario's " +
				"data with `none of the mocked ... requests to path ... have ...`.\n" +
				"- The method is `GET`, `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS` or `HEAD`, in capitals.",
			Examples: []string{"Then the mocked POST request to path /v1/parcels/PX-WEB-5302/returns named no-return on courier was not received"},
			Run:      notReceivedPath,
		},
		{
			ID: "mock.header.is", Keyword: "Then",
			Expr:     "the header {word} for mocked request named {word}[[ on {mockedService}]] is {string}",
			Doc:      "Check that the named request was received with a header of that value. " + headerJoins,
			Examples: []string{"Then the header X-Api-Key for mocked request named postcode-check is 'example-address-key'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{EqualTo: a.String(3)})
			},
		},
		{
			ID: "mock.headers.are", Keyword: "Then", Arg: core.ArgTable,
			Expr:  "the headers for mocked request named {word} on {mockedService} are:",
			Doc:   "Check that the named request was received with every header of the table, each with its value. " + headersJoin,
			Table: &core.TableDoc{Columns: []string{"header", "value"}, Note: "A row is a header's name and the value it must have."},
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses are:\n" +
				"  | X-Api-Key | example-address-key |\n" +
				"  | Accept    | application/json    |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headersCheck(sc, a, func(v string) headerMatcher { return headerMatcher{EqualTo: v} })
			},
		},
		{
			ID: "mock.header.matches", Keyword: "Then",
			Expr: "the header {word} for mocked request named {word} on {mockedService} matches {pattern}",
			Doc: "Check that the named request was received with a header that matches a regular expression.\n\n" +
				"- WireMock evaluates the expression, in Java syntax; it must match the whole value.\n" +
				"- " + headerJoins,
			Examples: []string{"Then the header Accept for mocked request named postcode-check on addresses matches ^application/json$"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{Matches: a.String(3)})
			},
		},
		{
			ID: "mock.headers.match", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the headers for mocked request named {word} on {mockedService} match:",
			Doc: "Check that the named request was received with headers that match regular expressions, a row each.\n\n" +
				"- WireMock evaluates the expressions, in Java syntax; each must match the whole value.\n" +
				"- " + headersJoin,
			Table: &core.TableDoc{
				Columns: []string{"header", "regular expression"},
				Note:    "A row is a header's name and a regular expression its value must match.",
			},
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses match:\n" +
				"  | X-Api-Key | example-.+         |\n" +
				"  | Accept    | application/json.* |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headersCheck(sc, a, func(v string) headerMatcher { return headerMatcher{Matches: v} })
			},
		},
		{
			ID: "mock.header.missing", Keyword: "Then",
			Expr:     "the header {word} for mocked request named {word} on {mockedService} is missing",
			Doc:      "Check that the named request was received without that header. " + headerJoins,
			Examples: []string{"Then the header Authorization for mocked request named postcode-check on addresses is missing"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return headerCheck(sc, a, a.String(0), a.String(1), headerMatcher{Absent: true})
			},
		},
		{
			ID: "mock.headers.missing", Keyword: "Then", Arg: core.ArgTable,
			Expr:  "the headers for mocked request named {word} on {mockedService} are missing:",
			Doc:   "Check that the named request was received without any of the headers the table names. " + headersJoin,
			Table: &core.TableDoc{Columns: []string{"header"}, Note: "A row names a header, in the table's only column."},
			Examples: []string{"Then the headers for mocked request named postcode-check on addresses are missing:\n" +
				"  | Authorization |\n" +
				"  | Cookie        |"},
			Run: headersMissing,
		},
		{
			ID: "mock.properties.are", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the payload properties for mocked request named {word}[[ on {mockedService}]] are:",
			Doc: "Check that the named request was received with a JSON body that has every property of the table, each with its value.\n\n" +
				"- A property is a JSONPath, like `deliverTo.postcode` or `$.lines[0].reference`; WireMock reads it.\n" +
				"- Values compare as text: `800` matches the number 800, and `\"10115\"` the string 10115. `undefined` means the body has " +
				"no such property, to check that a request leaves something out.\n" +
				"- " + bodyJoins,
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Note:    "A row is a property's JSONPath and the value it must have, or `undefined`.",
			},
			Examples: []string{"Then the payload properties for mocked request named collection on courier are:\n" +
				"  | reference          | PX-REG-1401 |\n" +
				"  | deliverTo.postcode | \"10115\"     |\n" +
				"  | recipient          | undefined   |"},
			Run: func(sc *core.Scenario, a core.Args) error { return bodyCheck(sc, a, (*pattern).withProperty) },
		},
		{
			ID: "mock.query.are", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the query parameters for mocked request named {word}[[ on {mockedService}]] are:",
			Doc: "Check that the named request was received with every query parameter of the table, each with its value.\n\n" +
				"- Name the request by its path (`the mocked ... request to path ...`): a request named by its whole URL matches its query already.\n" +
				"- `undefined` means the query has no such parameter.\n" +
				"- " + bodyJoins,
			Table: &core.TableDoc{
				Columns: []string{"parameter", "value"},
				Note:    "A row is a query parameter's name and the value it must have, or `undefined`.",
			},
			Examples: []string{"Then the query parameters for mocked request named collection on courier are:\n" +
				"  | slot      | same-day  |\n" +
				"  | reference | undefined |"},
			Run: func(sc *core.Scenario, a core.Args) error { return bodyCheck(sc, a, (*pattern).withQuery) },
		},
		{
			ID: "mock.form.are", Keyword: "Then", Arg: core.ArgTable,
			Expr: "the form fields for mocked request named {word}[[ on {mockedService}]] are:",
			Doc: "Check that the named request was received with a form-encoded body that has every field of the table, each with its value.\n\n" +
				"- `undefined` means the form has no such field.\n" +
				"- " + bodyJoins,
			Table: &core.TableDoc{
				Columns: []string{"field", "value"},
				Note:    "A row is a field's name and the value it must have, or `undefined`.",
			},
			Examples: []string{"Then the form fields for mocked request named pickup-notice on courier are:\n" +
				"  | reference | PX-WEB-5401 |\n" +
				"  | day       | Friday      |"},
			Run: func(sc *core.Scenario, a core.Args) error { return bodyCheck(sc, a, (*pattern).withField) },
		},
		noneStep("mock.query.none", "query parameters", "query parameter", "parameter", "a query parameter's name",
			"Then none of the mocked POST requests to path /v1/collections on courier have the query parameters:\n"+
				"  | slot      | same-day    |\n"+
				"  | reference | PX-REG-1402 |", (*pattern).withQuery),
		noneStep("mock.properties.none", "payload properties", "payload property", "property", "a property's JSONPath, like `deliverTo.postcode`,",
			"Then none of the mocked POST requests to path /v1/collections on courier have the payload properties:\n"+
				"  | reference | PX-REG-1301 |", (*pattern).withProperty),
		noneStep("mock.form.none", "form fields", "form field", "field", "a form field's name",
			"Then none of the mocked POST requests to path /v1/pickups on courier have the form fields:\n"+
				"  | reference | PX-WEB-5302 |", (*pattern).withField),
		signedStep(),
		webhookStep(),
	}, append(modelSteps(), agentSteps()...)...)
}

// noneStep checks that none of the requests to a path have every row of
// the table: the thing a scenario must not have caused, with its own data,
// since the requests of every scenario count.
func noneStep(id, what, one, column, row, example string, add func(*pattern, string, headerMatcher)) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then", Arg: core.ArgTable, Absence: true,
		Expr: "none of the mocked {word} requests to path {word}[[ on {mockedService}]] have the " + what + ":",
		Doc: "Check that the mocked service received no request with that method to a path, whatever its query string, that has " +
			"every " + one + " of the table, each with its value.\n\n" +
			"- Every request the mocked service received counts, other scenarios' too: put the scenario's own data in the table, " +
			"like its parcel's reference, next to what must not be there.\n" +
			"- A check that something did not happen proves little on its own: check what the scenario did send, too.\n" +
			"- `undefined` means the request has no such " + column + ".",
		Table: &core.TableDoc{
			Columns: []string{column, "value"},
			Note:    "A row is " + row + " and the value it has, or `undefined`.",
		},
		Examples: []string{example},
		Run: func(sc *core.Scenario, a core.Args) error {
			return noneReceived(sc, a, what, add)
		},
	}
}

// headerJoins, headersJoin and bodyJoins say that what a step checks stays
// with the named request: its later checks check it too.
const (
	headerJoins = "Later steps on the named request check this header too."
	headersJoin = "Later steps on the named request check these headers too."
	bodyJoins   = "Later steps on the named request check these too, so its counts count only the requests that have them."
)

func countStep(id, words string, ok func(got, n int) bool) core.StepDef {
	return core.StepDef{
		ID: id, Keyword: "Then",
		Expr:     "the mocked request named {word}[[ on {mockedService}]] was received " + words + " {int} time(s)",
		Doc:      fmt.Sprintf("Check that the mocked service received the named request %s that many times.", words),
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
		if p.Key != "url" {
			return fmt.Errorf("unknown mocked service property %q (supported: url)", p.Key)
		}
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
	lv, err := oaslevel.ParseMap(m, levelKeys)
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

func receivedPath(sc *core.Scenario, a core.Args) error {
	method, path, name := a.String(0), a.String(1), a.String(2)
	svc := a.Value(3).(*Service)
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD":
	default:
		return fmt.Errorf("Unsupported method: %s", method) //nolint:staticcheck // user-facing message
	}
	if strings.Contains(path, "?") {
		return fmt.Errorf("the path %s has a query string: check its query parameters with the query parameters step", path)
	}
	p := &pattern{Method: method, URL: path, PathOnly: true}
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

func notReceivedPath(sc *core.Scenario, a core.Args) error {
	svc, err := service(sc, a, 3)
	if err != nil {
		return err
	}
	method, path, name := a.String(0), a.String(1), a.String(2)
	p, err := pathPattern(method, path)
	if err != nil {
		return err
	}
	svc.requests[name] = p
	return verify(sc, svc, name, p, "exactly 0", func(got int) bool { return got == 0 })
}

// pathPattern is the requests with a method to a path, whatever their
// query string.
func pathPattern(method, path string) (*pattern, error) {
	switch method {
	case "GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD":
	default:
		return nil, fmt.Errorf("Unsupported method: %s", method) //nolint:staticcheck // user-facing message
	}
	if strings.Contains(path, "?") {
		return nil, fmt.Errorf("the path %s has a query string: put its query parameters in a step's table", path)
	}
	return &pattern{Method: method, URL: path, PathOnly: true}, nil
}

// noneReceived checks that none of the requests to a path have every row
// of the table. A failure lists the requests that do.
func noneReceived(sc *core.Scenario, a core.Args, what string, add func(*pattern, string, headerMatcher)) error {
	svc, err := service(sc, a, 2)
	if err != nil {
		return err
	}
	method, path := a.String(0), a.String(1)
	p, err := pathPattern(method, path)
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	for _, kv := range pairs {
		add(p, kv.Key, bodyValue(kv.Value))
	}
	got, err := svc.c.count(sc.Context(), p)
	if err != nil {
		return err
	}
	stateKey.Of(sc).last = &verification{service: svc.Name, pattern: p, want: "exactly 0", got: got}
	if got == 0 {
		return nil
	}
	msg := fmt.Sprintf("%d %s request(s) to path %s on %s have the %s of the table", got, method, path, svc.Name, what)
	if found, err := svc.c.find(sc.Context(), p); err == nil && len(found) > 0 {
		msg += ":"
		for i, r := range found {
			if i == 3 {
				msg += fmt.Sprintf("\n  and %d more", len(found)-3)
				break
			}
			msg += fmt.Sprintf("\n  %s %s", r.Method, r.URL)
		}
	}
	return core.Fail(msg+"\nPattern:\n"+p.describe(), "none", got)
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

// bodyCheck adds the table's rows to the named request (with add: its
// body's properties, its form's fields, its query's parameters) and checks
// that it was received.
func bodyCheck(sc *core.Scenario, a core.Args, add func(*pattern, string, headerMatcher)) error {
	svc, err := service(sc, a, 1)
	if err != nil {
		return err
	}
	name := a.String(0)
	p, err := named(svc, name)
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	for _, kv := range pairs {
		add(p, kv.Key, bodyValue(kv.Value))
	}
	return verify(sc, svc, name, p, "at least 1", func(got int) bool { return got >= 1 })
}

// bodyValue is the matcher of a table value: `undefined` for an absent
// property or field, otherwise its text, without the double quotes that
// make a value a string in other steps' tables.
func bodyValue(v string) headerMatcher {
	if v == "undefined" {
		return headerMatcher{Absent: true}
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return headerMatcher{EqualTo: v}
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
