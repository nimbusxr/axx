package rest

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
)

// target locates the request (or response) a step works on: the positions
// of its {ordinal} and {service} arguments, -1 when the step has none. An
// absent ordinal means the first request; an absent service the default
// (first registered) one.
type target struct{ ord, svc int }

func (t target) service(sc *core.Scenario, a core.Args) (*Service, error) {
	if t.svc >= 0 && a.Present(t.svc) {
		return a.Value(t.svc).(*Service), nil
	}
	return stateKey.Of(sc).services.Default()
}

func (t target) index(a core.Args) int {
	if t.ord < 0 {
		return 0
	}
	return a.IntOr(t.ord, 1) - 1
}

func (t target) request(sc *core.Scenario, a core.Args) (*Service, *Request, error) {
	svc, err := t.service(sc, a)
	if err != nil {
		return nil, nil, err
	}
	r, err := svc.request(t.index(a))
	if err != nil {
		return nil, nil, err
	}
	return svc, r, nil
}

// exchange returns the executed exchange of the targeted request.
func (t target) exchange(sc *core.Scenario, a core.Args) (*Exchange, error) {
	svc, r, err := t.request(sc, a)
	if err != nil {
		return nil, err
	}
	svc.mu.Lock()
	ex := r.exchange
	svc.mu.Unlock()
	if ex == nil {
		return nil, errors.New("Response not set") //nolint:staticcheck // user-facing message
	}
	return ex, nil
}

// family builds the two definitions that together provide the four forms
// of a step:
//
//	<head>[[ for {ordinal} ordered <noun>]]<tail>
//	<head> for[[ {ordinal} ordered]] <noun> on {service}<tail>
//
// nHead is the number of parameters in head, so the ordinal is argument
// nHead and the service argument nHead+1.
type family struct {
	id, keyword  string
	arg          core.ArgKind
	head, tail   string
	noun         string // "request" or "response"
	nHead        int
	doc          string
	example      string // default-service example
	namedExample string // named-service example
	run          func(sc *core.Scenario, a core.Args, t target) error
}

func (f family) defs() []core.StepDef {
	run := func(t target) core.StepFunc {
		return func(sc *core.Scenario, a core.Args) error { return f.run(sc, a, t) }
	}
	return []core.StepDef{
		{
			ID: f.id, Keyword: f.keyword, Arg: f.arg,
			Expr:     f.head + "[[ for {ordinal} ordered " + f.noun + "]]" + f.tail,
			Doc:      f.doc,
			Examples: []string{f.example},
			Run:      run(target{ord: f.nHead, svc: -1}),
		},
		{
			ID: f.id + ".on", Keyword: f.keyword, Arg: f.arg,
			Expr: f.head + " for[[ {ordinal} ordered]] " + f.noun + " on {service}" + f.tail,
			Doc: "`" + f.id + "` on a named service: `for " + f.noun + " on <service>` addresses the service's first " +
				"(default) " + f.noun + ", `for 2nd ordered " + f.noun + " on <service>` its second one. " +
				"Everything else works like `" + f.id + "`.",
			Examples: []string{f.namedExample},
			Run:      run(target{ord: f.nHead, svc: f.nHead + 1}),
		},
	}
}

const ordinalDoc = " Without an ordinal the step applies to the first (default) request of the service; " +
	"`for 2nd ordered request` picks the second one. Without `on {service}` it uses the default (first registered) service."

const respOrdinalDoc = " Without an ordinal the step checks the response of the first (default) request; " +
	"`for 2nd ordered response` the response of the second one. Without `on {service}` it uses the default (first registered) service."

func steps() []core.StepDef {
	var out []core.StepDef
	out = append(out, serviceSteps()...)
	out = append(out, requestSteps()...)
	out = append(out, responseSteps()...)
	return out
}

// ---- services ----

func serviceSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "rest.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} service with the following properties:",
			Doc: "Register a REST service. The first service registered in a scenario is the default one.\n\n" +
				"Properties (`${env:..}`/`${sys:..}` are expanded):\n\n" +
				"- `url` (required): the base URL requests are sent to, e.g. `http://localhost:8080`.\n" +
				"- `openapi`: the service's OpenAPI 3.0 or 3.1 specification, as a URL or a file path (resolved " +
				"against the `resources` roots). When set, every executed request and its response are validated " +
				"against it, and content example payloads come from it.",
			Examples:   []string{"Given the parcels service with the following properties:"},
			TableTypes: map[string]string{"openapi": "filepath"},
			Run:        addService,
		},
		{
			ID: "rest.openapi.levels", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the OpenAPI validation levels[[ on {service}]] are:",
			Doc: "Override OpenAPI validation levels for this scenario (on the default or the named service). " +
				"Each row is `validation key | level`; the level is `ERROR` (or its alias `FAIL`), `WARN`, `INFO` or " +
				"`IGNORE`. A key covers every more specific key: `validation.request.body` relaxes " +
				"`validation.request.body.schema.required` too, and the most specific configured key wins. " +
				"Rows are merged over `openapi.levels` from axx.yaml. See the pack documentation for the keys.",
			Examples: []string{"Given the OpenAPI validation levels are:"},
			Run:      setLevels,
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
		if !p.Null {
			props[p.Key] = sc.Suite().Interpolate(p.Value)
		}
	}
	u, ok := props["url"]
	if !ok {
		return errors.New(`Property "url" is required`) //nolint:staticcheck // user-facing message
	}
	return Context(sc).AddService(&Service{Name: a.String(0), URL: u, OpenAPI: props["openapi"]})
}

func setLevels(sc *core.Scenario, a core.Args) error {
	svc, err := target{ord: -1, svc: 0}.service(sc, a)
	if err != nil {
		return err
	}
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	levels := Levels{}
	for _, p := range pairs {
		lv, err := ParseLevel(p.Value)
		if err != nil {
			return fmt.Errorf("Invalid OpenAPI validation level %q for key %q. Supported levels: %s", p.Value, p.Key, supportedLevels) //nolint:staticcheck // user-facing message
		}
		levels[strings.TrimSpace(p.Key)] = lv
	}
	svc.mu.Lock()
	svc.levels = svc.levels.Merge(levels)
	svc.mu.Unlock()
	return nil
}

// ---- requests ----

func requestSteps() []core.StepDef {
	var out []core.StepDef
	out = append(out,
		core.StepDef{
			ID: "rest.request", Keyword: "Given",
			Expr: "a(n) {word} request to {word}[[ on {service}]]",
			Doc: "Add a request with a method and a path (optionally with a query string, e.g. `/api/parcels?sender=kestrel-books`) " +
				"to the default or the named service. This is the service's first (default) request; add more with the " +
				"ordered form. The path is appended to the service URL; an absolute URL replaces it.",
			Examples: []string{"Given a GET request to /api/parcels/PX-1001", "Given a DELETE request to /api/parcels/PX-1001 on parcels"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return addRequest(sc, a, target{ord: -1, svc: 2}, a.String(0), a.String(1))
			},
		},
		core.StepDef{
			ID: "rest.request.ordered", Keyword: "Given",
			Expr: "a {ordinal} ordered {word} request to {word}[[ on {service}]]",
			Doc: "Add the Nth request of a service. Requests are numbered in the order they are added: " +
				"the 1st ordered request is the default request, and the Nth can only be added once N-1 exist.",
			Examples: []string{"Given a 2nd ordered GET request to /api/parcels/PX-1001", "Given a 1st ordered POST request to /api/parcels on parcels"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return addRequest(sc, a, target{ord: 0, svc: 3}, a.String(1), a.String(2))
			},
		},
	)
	for _, f := range []family{
		{
			id: "rest.request.header", keyword: "Given", noun: "request",
			head: "the request header {word} is {string}", nHead: 2,
			doc: "Set a request header. `Content-Type` and `Accept` replace an earlier value; other headers may be " +
				"added more than once and are all sent." + ordinalDoc,
			example:      "Given the request header Content-Type is 'application/json'",
			namedExample: "Given the request header Accept is 'application/json' for 1st ordered request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return withRequest(sc, a, t, func(r *Request) error {
					r.addHeader(a.String(0), a.String(1))
					return nil
				})
			},
		},
		{
			id: "rest.request.headers", keyword: "Given", arg: core.ArgTable, noun: "request",
			head: "the request headers", tail: " are:",
			doc:          "Set request headers from a `name | value` table (a name may repeat)." + ordinalDoc,
			example:      "Given the request headers are:",
			namedExample: "Given the request headers for request on parcels are:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				rows, err := rows2(a.Table, "request headers")
				if err != nil {
					return err
				}
				return withRequest(sc, a, t, func(r *Request) error {
					for _, row := range rows {
						r.addHeader(row[0], row[1])
					}
					return nil
				})
			},
		},
		{
			id: "rest.request.payload.empty", keyword: "Given", noun: "request",
			head: "a request payload using a(n) {mimeType} empty content template", nHead: 1,
			doc: "Start the request payload from an empty JSON object `{}`, to be filled with the payload property " +
				"steps; no OpenAPI specification is needed. With `application/x-www-form-urlencoded` the properties " +
				"are sent form-encoded (nested objects and arrays as JSON text)." + ordinalDoc,
			example:      "Given a request payload using an application/json empty content template",
			namedExample: "Given a request payload using an application/json empty content template for request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return withRequest(sc, a, t, func(r *Request) error {
					return setPayload(r, a.String(0), "{}")
				})
			},
		},
		{
			id: "rest.request.payload.example", keyword: "Given", noun: "request",
			head: "a request payload using a(n) {mimeType} content example[[ named {string}]]", nHead: 2,
			doc: "Use a request body example of the service's OpenAPI specification as the payload: with " +
				"`named '<name>'` the example of that name, otherwise the first example in document order (or the " +
				"media type's single `example`). The example is looked up under the operation that matches the " +
				"request's method and path, for the given media type. An example with an `externalValue` is read " +
				"relative to the specification. Requires the service's `openapi` property and a request added first." + ordinalDoc,
			example:      "Given a request payload using an application/json content example named 'Standard parcel'",
			namedExample: "Given a request payload using an application/json content example for 1st ordered request on parcels",
			run:          examplePayload,
		},
		{
			id: "rest.request.property", keyword: "Given", noun: "request",
			head: "the request payload property {word} is {string}", nHead: 2,
			doc: "Set a payload property (a JSONPath such as `weightGrams`, `recipient.postcode` or `$.recipient.name`). " +
				"A value in double quotes inside the quotes (`'\"42\"'`) is always a string. Otherwise the value takes the " +
				"type of the current value (string, boolean, integer, number, object or array, parsed from JSON text); " +
				"a property that does not exist yet, or is null, gets the type the text reads as (`true`, `42`, `1.5`, " +
				"`{...}`, `[...]`, else a string). Requires a payload step first." + ordinalDoc,
			example:      "Given the request payload property sender is 'kestrel-books'",
			namedExample: "Given the request payload property serviceLevel is 'EXPRESS' for request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return modifyPayload(sc, a, t, func(doc any) error { return jvalue.SetRequestProperty(doc, a.String(0), a.String(1)) })
			},
		},
		{
			id: "rest.request.properties", keyword: "Given", arg: core.ArgTable, noun: "request",
			head: "the request payload properties", tail: " are:",
			doc: "Set payload properties from a `path | value` table, row by row, like the single-property step. " +
				"`null` sets JSON null and `undefined` removes the property (any case); write `\"null\"` or " +
				"`\"undefined\"` in double quotes for the strings." + ordinalDoc,
			example:      "Given the request payload properties are:",
			namedExample: "Given the request payload properties for 1st ordered request on parcels are:",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				return modifyPayload(sc, a, t, func(doc any) error {
					for _, p := range pairs {
						if err := jvalue.ApplyRequestTableRow(doc, p.Key, p.Value); err != nil {
							return err
						}
					}
					return nil
				})
			},
		},
		{
			id: "rest.request.property.null", keyword: "Given", noun: "request",
			head: "the request payload property {word} is null", nHead: 1,
			doc:          "Set an existing payload property to JSON null." + ordinalDoc,
			example:      "Given the request payload property recipient.street is null",
			namedExample: "Given the request payload property recipient.street is null for request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return modifyPayload(sc, a, t, func(doc any) error { return jvalue.SetRequestPropertyNull(doc, a.String(0)) })
			},
		},
	} {
		out = append(out, f.defs()...)
	}
	out = append(out, core.StepDef{
		ID: "rest.execute", Keyword: "When",
		Expr: "the[[ {ordinal} ordered]] request is executed[[ on {service}]]",
		Doc: "Send a request and keep its response for the response steps. With an OpenAPI specification the " +
			"request and the response are validated after sending: findings at level ERROR fail the step (all of " +
			"them are listed with their keys), WARN and INFO are logged. The payload is sent as is, form-encoded " +
			"for `application/x-www-form-urlencoded`; without a Content-Type header the payload's media type is " +
			"used. The request honors the step timeout. A request can be executed once.",
		Examples: []string{"When the request is executed", "When the 2nd ordered request is executed on parcels"},
		Run: func(sc *core.Scenario, a core.Args) error {
			t := target{ord: 0, svc: 1}
			svc, err := t.service(sc, a)
			if err != nil {
				return err
			}
			return execute(sc, svc, t.index(a))
		},
	})
	return out
}

func addRequest(sc *core.Scenario, a core.Args, t target, method, path string) error {
	svc, err := t.service(sc, a)
	if err != nil {
		return err
	}
	r, err := svc.requestOrAdd(t.index(a))
	if err != nil {
		return err
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	if r.Method != "" {
		return errors.New("Method already set") //nolint:staticcheck // user-facing message
	}
	if r.Path != "" {
		return errors.New("Path already set") //nolint:staticcheck // user-facing message
	}
	r.Method = strings.ToUpper(method)
	r.Path = path
	return nil
}

// withRequest runs fn on the targeted request, under the service lock.
func withRequest(sc *core.Scenario, a core.Args, t target, fn func(r *Request) error) error {
	svc, r, err := t.request(sc, a)
	if err != nil {
		return err
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return fn(r)
}

func setPayload(r *Request, mimeType, payload string) error {
	if r.MimeType != "" {
		return errors.New("MIME type already set") //nolint:staticcheck // user-facing message
	}
	if r.Payload != nil {
		return errors.New("Payload already set") //nolint:staticcheck // user-facing message
	}
	r.MimeType = mimeType
	r.Payload = &payload
	return nil
}

func examplePayload(sc *core.Scenario, a core.Args, t target) error {
	svc, r, err := t.request(sc, a)
	if err != nil {
		return err
	}
	mimeType := a.String(0)
	name := a.String(1)
	svc.mu.Lock()
	method, path, hasMime := r.Method, r.Path, r.MimeType != ""
	svc.mu.Unlock()
	if hasMime {
		return errors.New("MIME type already set") //nolint:staticcheck // user-facing message
	}
	if svc.OpenAPI == "" {
		return fmt.Errorf("service %s has no openapi property; content examples come from its OpenAPI specification", svc.Name)
	}
	set, err := load(sc.Suite())
	if err != nil {
		return err
	}
	sp, err := loadSpec(sc, set, svc)
	if err != nil {
		return err
	}
	payload, err := sp.requestExample(sc, set, method, path, mimeType, name)
	if err != nil {
		return err
	}
	svc.mu.Lock()
	defer svc.mu.Unlock()
	return setPayload(r, mimeType, payload)
}

// modifyPayload parses the targeted request's payload, applies fn and
// stores the result (json-smart parsing, Jayway updates, json-smart
// serialization).
func modifyPayload(sc *core.Scenario, a core.Args, t target, fn func(doc any) error) error {
	return withRequest(sc, a, t, func(r *Request) error {
		if r.Payload == nil {
			return errors.New("Request payload is not set. Use a payload step to initialize the payload first" + //nolint:staticcheck // user-facing message
				" (e.g., 'Given a request payload using a(n) {mimeType} content example'" +
				" or 'Given a request payload using a(n) {mimeType} empty content template').")
		}
		switch r.MimeType {
		case core.MimeJSON, core.MimeTextJSON, core.MimeForm:
		default:
			return fmt.Errorf("Request content type is not supported for payload property validation: %s", r.MimeType) //nolint:staticcheck // user-facing message
		}
		doc, err := jsonx.Parse(*r.Payload)
		if err != nil {
			return fmt.Errorf("the request payload is not JSON: %w", err)
		}
		if err := fn(doc); err != nil {
			return err
		}
		text, err := jsonx.Marshal(doc)
		if err != nil {
			return err
		}
		r.Payload = &text
		return nil
	})
}

// rows2 returns the rows of a two-column table (names may repeat).
func rows2(t *core.Table, what string) ([][2]string, error) {
	if t == nil {
		return nil, fmt.Errorf("a data table is required")
	}
	out := make([][2]string, 0, len(t.Rows))
	for i, row := range t.Rows {
		if len(row) != 2 {
			return nil, fmt.Errorf("%s: data table row %d has %d cells; expected 2 (name | value)", what, i+1, len(row))
		}
		out = append(out, [2]string{row[0], row[1]})
	}
	return out, nil
}
