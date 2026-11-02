package rest

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/compat/jsonx"
	"github.com/nimbusxr/axx/internal/compat/jvalue"
	"github.com/nimbusxr/axx/internal/secrets"
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
// nHead and the service argument nHead+1. A leading family also has the
// form with its ordinal first, as the status and execute steps put it:
//
//	the {ordinal} ordered <head without its "the">[[ on {service}]]<tail>
type family struct {
	id, keyword string
	since       string // the axx version that introduced the step, if after 0.1.0
	arg         core.ArgKind
	head, tail  string
	noun        string // "request" or "response"
	nHead       int
	leading     bool
	// bare says the value in head, a {string}, may also be an integer without
	// quotes: is 2500, for is '2500'.
	bare bool
	// textArgs and typedArgs are head's arguments that hold values
	// (core.StepDef.TextArgs, TypedArgs), which take undefined.
	textArgs, typedArgs []int
	doc                 string   // what the step does, in a sentence
	details             []string // more of it, a short sentence or two each
	table               *core.TableDoc
	example             string // default-service example
	namedExample        string // named-service example
	run                 func(sc *core.Scenario, a core.Args, t target) error
}

func (f family) defs() []core.StepDef {
	run := func(t target) core.StepFunc {
		return func(sc *core.Scenario, a core.Args) error { return f.run(sc, a, t) }
	}
	also := func(expr string) []string {
		if !f.bare {
			return nil
		}
		return []string{strings.Replace(expr, "{string}", "{integer}", 1)}
	}
	undefined := len(f.typedArgs) > 0
	defs := []core.StepDef{
		{
			ID: f.id, Keyword: f.keyword, Arg: f.arg, Since: f.since,
			Expr:      f.head + "[[ for {ordinal} ordered " + f.noun + "]]" + f.tail,
			Also:      also(f.head + "[[ for {ordinal} ordered " + f.noun + "]]" + f.tail),
			Doc:       docList(f.doc, append(append([]string{}, f.details...), f.targetDoc()...)...),
			Table:     f.table,
			Examples:  []string{f.example},
			TextArgs:  f.textArgs,
			TypedArgs: f.typedArgs, TypedArgsUndefined: undefined,
			Run: run(target{ord: f.nHead, svc: -1}),
		},
		{
			ID: f.id + ".on", Keyword: f.keyword, Arg: f.arg, Since: f.since,
			Expr: f.head + " for[[ {ordinal} ordered]] " + f.noun + " on {service}" + f.tail,
			Also: also(f.head + " for[[ {ordinal} ordered]] " + f.noun + " on {service}" + f.tail),
			Doc: "`" + f.id + "` on a named service: `for " + f.noun + " on <service>` addresses the service's first " +
				"(default) " + f.noun + ", `for 2nd ordered " + f.noun + " on <service>` its second one. " +
				"Everything else works like `" + f.id + "`.",
			Table:     f.table,
			Examples:  []string{f.namedExample},
			TextArgs:  f.textArgs,
			TypedArgs: f.typedArgs, TypedArgsUndefined: undefined,
			Run: run(target{ord: f.nHead, svc: f.nHead + 1}),
		},
	}
	if f.leading {
		named := run(target{ord: f.nHead, svc: f.nHead + 1})
		rest := strings.TrimPrefix(f.head, "the ")
		defs = append(defs, core.StepDef{
			ID: f.id + ".leading", Keyword: f.keyword, Arg: f.arg, Since: "0.3.0",
			Expr: "the {ordinal} ordered " + rest + "[[ on {service}]]" + f.tail,
			Also: also("the {ordinal} ordered " + rest + "[[ on {service}]]" + f.tail),
			Doc: "`" + f.id + "` with its ordinal first, as `the 2nd ordered " + f.noun + " ...` steps say it: " +
				"the same step as `" + f.head + " for 2nd ordered " + f.noun + f.tail + "`.",
			Table:    f.table,
			Examples: []string{strings.Replace(f.example, " the ", " the 2nd ordered ", 1)},
			// The ordinal comes first here, ahead of the head's arguments.
			TextArgs:  shifted(f.textArgs),
			TypedArgs: shifted(f.typedArgs), TypedArgsUndefined: undefined,
			Run: func(sc *core.Scenario, a core.Args) error {
				// The family's steps read the ordinal after the head's parameters.
				list := append(append([]core.Arg{}, a.List[1:f.nHead+1]...), a.List[0])
				a.List = append(list, a.List[f.nHead+1:]...)
				return named(sc, a)
			},
		})
	}
	return defs
}

// shifted is the places of arguments one further on.
func shifted(places []int) []int {
	var out []int
	for _, p := range places {
		out = append(out, p+1)
	}
	return out
}

// docList is a step's Doc: what it does, in a sentence, then more of it as
// a list.
func docList(first string, items ...string) string {
	if len(items) == 0 {
		return first
	}
	return first + "\n\n- " + strings.Join(items, "\n- ")
}

// targetDoc says which request (or response) and which service the first
// step of a family works on.
func (f family) targetDoc() []string {
	which := "Without an ordinal it applies to the service's first (default) request; `for 2nd ordered request`, to its second."
	if f.noun == "response" {
		which = "Without an ordinal it checks the response to the first (default) request; `for 2nd ordered response`, " +
			"the response to the second."
	}
	return []string{which, "It uses the default service, the first one registered; `" + f.id + ".on` names a service."}
}

// serviceDoc says which service a step uses when it names none.
const serviceDoc = "Without `on {service}` it uses the default service, the first one registered."

// levelsNote is the note on the table of OpenAPI validation levels.
const levelsNote = "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
	"The key must be one the pack reports, or a prefix of such keys, like `validation.request.body`: " +
	"any other fails the step, which names the closest keys."

func steps() []core.StepDef {
	var out []core.StepDef
	out = append(out, serviceSteps()...)
	out = append(out, requestSteps()...)
	out = append(out, responseSteps()...)
	out = append(out, authSteps()...)
	return out
}

// ---- services ----

func serviceSteps() []core.StepDef {
	return []core.StepDef{
		{
			ID: "rest.service", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the {word} service with the following properties:",
			Also: []string{"a(n) {word} service with the following properties:"},
			Doc: docList("Register a REST service: where its requests go, and the OpenAPI specification they are checked against.",
				"The first service registered in a scenario is the default one.",
				"With an `openapi` specification, every request executed and its response are validated against it, "+
					"and request payloads can start from its examples.",
				"Values can use `${env:…}` and `${sys:…}`, which are expanded."),
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Takes: "the base URL requests go to, like `http://localhost:8400`; a request's path is added to it", Required: true},
					{Name: "openapi", Takes: "the service's OpenAPI 3.0 or 3.1 specification: a URL, or a file of the project (relative to " +
						"the `resources` directories or to axx.yaml's directory)"},
				},
				Note: "Any other property fails the step.",
			},
			Examples: []string{
				"Given the parcels service with the following properties:\n" +
					"  | url     | http://localhost:8400              |\n" +
					"  | openapi | http://localhost:8400/openapi.json |",
			},
			TableTypes: map[string]string{"openapi": "filepath"},
			Run:        addService,
		},
		{
			ID: "rest.openapi.levels", Keyword: "Given", Arg: core.ArgTable,
			Expr: "the OpenAPI validation levels[[ on {service}]] are:",
			Doc: docList("Set the level of OpenAPI validation findings for this scenario, on the default or the named service.",
				"A key also sets the keys below it: `validation.request.body` relaxes `validation.request.body.schema.required` "+
					"too. The most specific key set wins.",
				"The rows are merged over `openapi.levels` of axx.yaml.",
				"The pack's documentation lists the keys, and what each level does."),
			Table: &core.TableDoc{Columns: []string{"validation key", "level"}, Note: levelsNote},
			Examples: []string{
				"Given the OpenAPI validation levels are:\n" +
					"  | validation.request.body.schema.maximum | IGNORE |\n" +
					"  | validation.response.header.missing     | WARN   |",
				"Given the OpenAPI validation levels on parcels are:\n" +
					"  | validation.request.body.schema.required | WARN |",
			},
			Run: setLevels,
		},
	}
}

// serviceProperties are the properties of a REST service.
var serviceProperties = []string{"url", "openapi"}

func addService(sc *core.Scenario, a core.Args) error {
	pairs, err := a.Table.Pairs()
	if err != nil {
		return err
	}
	props := map[string]string{}
	for _, p := range pairs {
		if !slices.Contains(serviceProperties, p.Key) {
			return fmt.Errorf("unknown service property %q (supported: %s)", p.Key, strings.Join(serviceProperties, ", "))
		}
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
		if err := levelKeys.Check(strings.TrimSpace(p.Key)); err != nil {
			return err
		}
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
			Doc: docList("Add a request to the default or the named service: its method, and its path below the service's `url`.",
				"The path can have a query string, like `/api/parcels?sender=kestrel-books`. A whole URL replaces the service's `url`.",
				"This is the service's first (default) request; the ordered form adds more."),
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
			head: "the request header {word} is {string}", nHead: 2, textArgs: []int{1},
			doc: "Set a request header.",
			details: []string{
				"`Content-Type` and `Accept` replace an earlier value; other headers may be added more than once, and are all sent.",
			},
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
			doc: "Set request headers, a row each.",
			details: []string{
				"A name may repeat. Like the single-header step, `Content-Type` and `Accept` replace an earlier value, " +
					"and other headers are all sent.",
			},
			table: &core.TableDoc{
				Columns: []string{"header", "value"}, Note: "A row is a header's name and its value.",
				Values: core.TextValues,
			},
			example: "Given the request headers are:\n" +
				"  | Accept          | application/json |\n" +
				"  | Accept-Language | de-DE            |",
			namedExample: "Given the request headers for request on parcels are:\n" +
				"  | Content-Type | application/json |\n" +
				"  | Accept       | application/json |",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				rows, err := textRows(a.Table, "request headers")
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
			doc: "Start the request payload from an empty JSON object, `{}`, for the payload property steps to fill.",
			details: []string{
				"It needs no OpenAPI specification.",
				"With `application/x-www-form-urlencoded`, the properties are sent form-encoded, and nested objects and arrays as JSON text.",
				"A request takes one payload step.",
			},
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
			doc: "Start the request payload from a request body example of the service's OpenAPI specification.",
			details: []string{
				"With `named '<name>'` it is the example of that name; without, the first example in document order, " +
					"or the media type's single `example`.",
				"The example is looked up under the operation that matches the request's method and path, for the media type.",
				"An example with an `externalValue` is read relative to the specification.",
				"The service needs its `openapi` property, and the request must be added first.",
				"A request takes one payload step.",
			},
			example:      "Given a request payload using an application/json content example named 'Standard parcel'",
			namedExample: "Given a request payload using an application/json content example for 1st ordered request on parcels",
			run:          examplePayload,
		},
		{
			id: "rest.request.payload.resource", keyword: "Given", noun: "request",
			head: "a request payload using a(n) {mimeType} {filepath} resource", nHead: 2,
			doc: "Start the request payload from a file of the project, such as a fixture factory's output: it is sent with the media type.",
			details: []string{
				"The file is found like other files steps name: relative to the `resources` directories, or to axx.yaml's directory.",
				"It is sent as it is, and a JSON payload can then be changed with the payload property steps. " +
					"With `application/x-www-form-urlencoded`, the file is a JSON object whose properties are sent form-encoded.",
				"It needs no OpenAPI specification.",
				"A request takes one payload step.",
			},
			example:      "Given a request payload using an application/json requests/order-7731.json resource",
			namedExample: "Given a request payload using an application/json requests/order-7732.json resource for 2nd ordered request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				path, err := sc.ResolvePath(a.String(1))
				if err != nil {
					return err
				}
				body, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("cannot read the payload file %s: %w", a.String(1), err)
				}
				return withRequest(sc, a, t, func(r *Request) error {
					return setPayload(r, a.String(0), string(body))
				})
			},
		},
		{
			id: "rest.request.property", keyword: "Given", noun: "request",
			leading: true,
			bare:    true,
			head:    "the request payload property {word} is {string}", nHead: 2, typedArgs: []int{1},
			doc: "Set a property of the request payload, by its JSONPath, like `weightGrams`, `recipient.postcode` or `$.recipient.name`.",
			details: []string{
				"The value means what it says, whatever the property held before: `'1200'` is an integer, `'1.5'` a decimal, " +
					"`'true'` a boolean, `'null'` null, `'{...}'` and `'[...]'` JSON, `'\"10115\"'`, in double quotes inside the " +
					"quotes, the string, and other text, like `'EXPRESS'`, a string. `'undefined'` removes the property.",
				"The single quotes are the step's own, not the value's: `is 1200` says what `is '1200'` says.",
				"A property the payload lacks is added; one inside an object the payload lacks creates the object: " +
					"`recipient.name` in a payload without `recipient`. A path through an array (`items[2].sku`) needs the array.",
				"`null` and `undefined` change a property the payload has.",
				"`${env:…}` and `${sys:…}` expand inside the value, which stays text, whatever they expand to.",
				"A payload step must come first.",
			},
			example:      "Given the request payload property sender is 'kestrel-books'",
			namedExample: "Given the request payload property serviceLevel is 'EXPRESS' for request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				path := a.String(0)
				v, err := payloadValue(sc, a.String(1), false)
				if err != nil {
					return fmt.Errorf("%s: %w", path, core.InStepText(err))
				}
				return modifyPayload(sc, a, t, func(doc any) error {
					return missingParent(doc, path, jvalue.SetValue(doc, path, v))
				})
			},
		},
		{
			id: "rest.request.properties", keyword: "Given", arg: core.ArgTable, noun: "request",
			leading: true,
			head:    "the request payload properties", tail: " are:",
			doc: "Set properties of the request payload, a row each, in order, like the single-property step.",
			details: []string{
				"A value is set as written, whatever the property held before.",
				"A property inside an object the payload lacks creates the object, so a table can list " +
					"`recipient.name` and `recipient.postcode` on an empty payload.",
				"`null` sets a property the payload has to JSON null, and `undefined` removes it.",
			},
			table: &core.TableDoc{
				Columns: []string{"JSONPath", "value"}, Note: "A row is a property's JSONPath and its value.",
				Values: core.JSONValues, Undefined: true,
			},
			example: "Given the request payload properties are:\n" +
				"  | reference          | PX-4101 |\n" +
				"  | weightGrams        | 1200    |\n" +
				"  | recipient.postcode | \"53111\" |",
			namedExample: "Given the request payload properties for 1st ordered request on parcels are:\n" +
				"  | sender           | lark-ceramics |\n" +
				"  | recipient.street | undefined     |",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				values := make([]core.Value, len(pairs))
				for i, p := range pairs {
					if values[i], err = payloadValue(sc, p.Value, p.Null); err != nil {
						return fmt.Errorf("%s: %w", p.Key, err)
					}
				}
				return modifyPayload(sc, a, t, func(doc any) error {
					for i, p := range pairs {
						if err := jvalue.SetValue(doc, p.Key, values[i]); err != nil {
							return missingParent(doc, p.Key, err)
						}
					}
					return nil
				})
			},
		},
		{
			id: "rest.request.property.null", keyword: "Given", noun: "request",
			leading: true,
			head:    "the request payload property {word} is null", nHead: 1,
			doc:          "Set a property of the request payload to JSON null, adding it if the payload lacks it, as a table's `null` does.",
			example:      "Given the request payload property recipient.street is null",
			namedExample: "Given the request payload property recipient.street is null for request on parcels",
			run: func(sc *core.Scenario, a core.Args, t target) error {
				return modifyPayload(sc, a, t, func(doc any) error {
					null, err := core.ParseValue("null", false, false)
					if err != nil {
						return err
					}
					return missingParent(doc, a.String(0), jvalue.SetValue(doc, a.String(0), null))
				})
			},
		},
	} {
		out = append(out, f.defs()...)
	}
	out = append(out, core.StepDef{
		ID: "rest.execute", Keyword: "When",
		Expr: "the[[ {ordinal} ordered]] request is executed[[ on {service}]]",
		Doc: docList("Send a request, and keep its response for the response steps.",
			"With an OpenAPI specification, the request and its response are validated after sending. Findings at level "+
				"`ERROR` fail the step, which lists them all with their keys; `WARN` and `INFO` findings are logged.",
			"The payload is sent as it is, or form-encoded for `application/x-www-form-urlencoded`.",
			"Without a `Content-Type` header, the request has the payload's media type; without an `Accept` header, `*/*`.",
			"A GET or HEAD request follows redirects, and a request with another method a `303 See Other` (with a GET): "+
				"the response steps then check the response it leads to. Other redirects are the response, with their `Location` header.",
			"No cookies are kept: a `Set-Cookie` response header is not sent back. Send one with the `Cookie` request header.",
			"The request honors the step timeout, and is executed once.",
			"Without an ordinal it sends the service's first (default) request; `the 2nd ordered request`, its second.",
			serviceDoc),
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

// missingParent explains a PathNotFoundException from setting a request
// payload property: a property can be added, but not inside one the payload
// does not have. It names the first part of path that is missing, so the
// message says what to add or start from; the Java exception stays at the
// end. Any other error comes back as it is.
func missingParent(doc any, path string, err error) error {
	var je *jsonx.Error
	if err == nil || !errors.As(err, &je) || !je.Kind.Is(jsonx.PathNotFound) {
		return err
	}
	missing := ""
	for p := path; ; {
		parent, _ := jsonx.ParentAndKey(p)
		if parent == "" || parent == "$" {
			break
		}
		if ok, _ := jsonx.Exists(doc, parent); ok {
			break
		}
		missing, p = parent, parent
	}
	if missing == "" {
		return fmt.Errorf("the request payload has no place for %s: %w", path, err)
	}
	if !strings.HasPrefix(path, "$") {
		missing = strings.TrimPrefix(missing, "$.") // as the step wrote it
	}
	return fmt.Errorf("the request payload has no %s, so %s cannot be set inside it: start the payload from one that has it "+
		"(like `a request payload using an application/json content example`), or set %s first: %w", missing, path, missing, err)
}

// payloadValue reads a value a step sets in the request payload: it means
// what it says (ADR 0014). ${env:..} and ${sys:..} references expand inside
// a string or JSON as written, so a reference written as text stays a
// string, whatever it expands to.
func payloadValue(sc *core.Scenario, text string, empty bool) (core.Value, error) {
	v, err := core.ParseValue(text, empty, true)
	if err != nil || !strings.Contains(text, "${") {
		return v, err
	}
	if s, ok := v.Str(); ok {
		return core.ParseValue(`"`+secrets.Expand(sc, s)+`"`, false, false)
	}
	return core.ParseValue(secrets.Expand(sc, text), false, false)
}

// textRows returns the rows of a two-column table whose values are text
// (ADR 0014): an empty cell, or a value that reads as anything else, fails
// with the form to write.
func textRows(t *core.Table, what string) ([][2]string, error) {
	rows, err := rows2(t, what)
	if err != nil {
		return nil, err
	}
	for i, row := range rows {
		v, _, err := core.ParseText(row[1], row[1] == "", false)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", row[0], err)
		}
		rows[i][1] = v
	}
	return rows, nil
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
