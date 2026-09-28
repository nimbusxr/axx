package jsonrpc

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/oaslevel"
	"github.com/nimbusxr/axx/internal/secrets"
)

const levelsNote = "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
	"The key must be one the pack reports, or a prefix of such keys, like `validation.params`: " +
	"any other fails the step, which names the closest keys."

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".service", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} jsonrpc service with the following properties:",
			Doc:  "Register a JSON-RPC 2.0 service, which takes calls over HTTP, under a name.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "url", Required: true, Takes: "the URL calls are posted to"},
					{Name: "header.<name>", Takes: "a header sent with every call, such as `header.Authorization`"},
					{Name: "timeout", Takes: "how long a call may take, like `5s`", Default: "10s"},
					{Name: "openrpc", Takes: "the service's OpenRPC document, which its calls are checked against: a file of the project, a URL, or `rpc.discover` to ask the service for it"},
				},
				Note: "Values are expanded (`${env:..}`, `${sys:..}`, `${token:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{
				"Given the depots jsonrpc service with the following properties:\n" +
					"  | url     | http://localhost:8400/rpc |\n" +
					"  | openrpc | rpc.discover              |",
				"Given the depots jsonrpc service with the following properties:\n" +
					"  | url                  | https://depots.parcels.example/rpc |\n" +
					"  | header.Authorization | Bearer ${env:DEPOTS_TOKEN}         |\n" +
					"  | openrpc              | openrpc/depots.json                |",
			},
			TableTypes: map[string]string{"openrpc": "filepath"},
			Run:        register,
		},
		{
			ID: Name + ".call", Keyword: "When", Since: since,
			Expr:     "the {word} method is called on the {word} jsonrpc service",
			Doc:      "Call a method without params.",
			Examples: []string{"When the depot.list method is called on the depots jsonrpc service"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callOn(sc, a.String(1), a.String(0), nil, nil)
			},
		},
		{
			ID: Name + ".call.params", Keyword: "When", Arg: core.ArgTable, Since: since,
			Expr: "the {word} method is called on the {word} jsonrpc service with the following params:",
			Doc:  "Call a method with params by name, from the table.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note: "Each row is a param, or a path into one (`address.postcode`), and its value: numbers and booleans are JSON's, " +
					"double quotes make a value text, `null` is null and `undefined` leaves it out.",
			},
			Examples: []string{"When the parcel.hold method is called on the depots jsonrpc service with the following params:\n" +
				"  | reference | PX-RPC-7101 |\n" +
				"  | until     | 2026-10-02  |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callOn(sc, a.String(1), a.String(0), a.Table, nil)
			},
		},
		{
			ID: Name + ".call.doc", Keyword: "When", Arg: core.ArgDocString, Since: since,
			Expr:     "the {word} method is called on the {word} jsonrpc service with the params:",
			Doc:      "Call a method with the params of the doc string: an object, by name, or an array, by position.",
			Examples: []string{"When the parcel.get method is called on the depots jsonrpc service with the params:\n  \"\"\"\n  [\"PX-RPC-7102\"]\n  \"\"\""},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callOn(sc, a.String(1), a.String(0), nil, a.DocString)
			},
		},
		{
			ID: Name + ".result", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} jsonrpc service's result has the following properties:",
			Doc:  "Check the result of the service's last call.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the result (a field name, a dotted path or a JSONPath) and its value, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then the depots jsonrpc service's result has the following properties:\n" +
				"  | reference | PX-RPC-7101 |\n" +
				"  | status    | ON_HOLD     |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				_, c, err := result(sc, a.String(0))
				if err != nil {
					return err
				}
				return secrets.Hide(sc, jsonassert.Properties(string(c.result), a.Table, false))
			},
		},
		{
			ID: Name + ".result.is", Keyword: "Then", Since: since,
			Expr:     "the {word} jsonrpc service's result is {string}",
			Doc:      "Check the whole result of the service's last call: a text's text, or the JSON of any other result.",
			Examples: []string{"Then the depots jsonrpc service's result is 'true'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				s, c, err := result(sc, a.String(0))
				if err != nil {
					return err
				}
				got := string(c.result)
				var text string
				if json.Unmarshal(c.result, &text) == nil {
					got = text
				}
				if want := secrets.Expand(sc, a.String(1)); got != want {
					return secrets.Hide(sc, core.Fail(fmt.Sprintf("The %s jsonrpc service's result of %s is %s, not %q", s.name, c.method, c.result, want), want, got))
				}
				return nil
			},
		},
		{
			ID: Name + ".error", Keyword: "Then", Since: since,
			Expr: "the {word} jsonrpc service answered the error {int}[[ with a message containing {string}]]",
			Doc:  "Check the error code the service's last call answered, and the text its message contains.",
			Examples: []string{
				"Then the depots jsonrpc service answered the error -32602",
				"Then the depots jsonrpc service answered the error -32010 with a message containing 'out for delivery'",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				s, c, err := failure(sc, a.String(0))
				if err != nil {
					return err
				}
				if want := a.Int(1); c.code != want {
					return secrets.Hide(sc, core.Fail(fmt.Sprintf("The %s jsonrpc service answered the error %d (%q) to %s, not %d", s.name, c.code, c.text, c.method, want), want, c.code))
				}
				if a.Present(2) {
					if want := secrets.Expand(sc, a.String(2)); !strings.Contains(c.text, want) {
						return secrets.Hide(sc, core.Fail(fmt.Sprintf("The %s jsonrpc service answered the error %d (%q) to %s, whose message does not contain %q", s.name, c.code, c.text, c.method, want), want, c.text))
					}
				}
				return nil
			},
		},
		{
			ID: Name + ".error.properties", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} jsonrpc service's error has the following properties:",
			Doc:  "Check the error the service's last call answered: its `code`, its `message` and its `data`.",
			Table: &core.TableDoc{
				Columns: []string{"path", "value"},
				Note:    "Each row is a path into the error object (`code`, `message`, `data.reason`) and its value, compared as text: `null` for null and `undefined` for absent.",
			},
			Examples: []string{"Then the depots jsonrpc service's error has the following properties:\n" +
				"  | code        | -32010           |\n" +
				"  | data.status | OUT_FOR_DELIVERY |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				_, c, err := failure(sc, a.String(0))
				if err != nil {
					return err
				}
				return secrets.Hide(sc, jsonassert.Properties(string(c.err), a.Table, false))
			},
		},
		{
			ID: Name + ".openrpc.levels", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the OpenRPC validation levels are:",
			Doc: "Set the level of OpenRPC validation findings for this scenario, on every JSON-RPC service it calls.\n\n" +
				"- A key also sets the keys below it: `validation.params` relaxes `validation.params.schema.required` too. The most specific key set wins.\n" +
				"- The rows are merged over `packs.jsonrpc.openrpc.levels` of axx.yaml.\n" +
				"- The pack's documentation lists the keys, and what each level does.",
			Table: &core.TableDoc{Columns: []string{"validation key", "level"}, Note: levelsNote},
			Examples: []string{"Given the OpenRPC validation levels are:\n" +
				"  | validation.params | IGNORE |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				pairs, err := a.Table.Pairs()
				if err != nil {
					return err
				}
				levels := oaslevel.Levels{}
				for _, p := range pairs {
					key := strings.TrimSpace(p.Key)
					if err := levelKeys.Check(key); err != nil {
						return err
					}
					lv, err := oaslevel.Parse(p.Value)
					if err != nil {
						return fmt.Errorf("invalid OpenRPC validation level %q for key %q; supported levels: %s", p.Value, p.Key, oaslevel.Supported)
					}
					levels[key] = lv
				}
				st := scenarioLevels.Of(sc)
				st.mu.Lock()
				st.levels = st.levels.Merge(levels)
				st.mu.Unlock()
				return nil
			},
		},
	}
}

func callOn(sc *core.Scenario, serviceName, method string, t *core.Table, doc *core.DocString) error {
	s, err := get(sc, serviceName)
	if err != nil {
		return err
	}
	ps, err := params(sc, t, doc)
	if err != nil {
		return err
	}
	return s.invoke(sc, method, ps, t != nil)
}

func lastCall(sc *core.Scenario, name string) (*service, *call, error) {
	s, err := get(sc, name)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	c := s.last
	s.mu.Unlock()
	if c == nil {
		return nil, nil, fmt.Errorf("the %s jsonrpc service has not been called in this scenario", name)
	}
	return s, c, nil
}

// result is the service's last call, which must have answered a result.
func result(sc *core.Scenario, name string) (*service, *call, error) {
	s, c, err := lastCall(sc, name)
	if err != nil {
		return nil, nil, err
	}
	if c.result == nil {
		return nil, nil, secrets.Hide(sc, core.Failf("The %s jsonrpc service answered the error %d (%q) to %s, not a result", s.name, c.code, c.text, c.method))
	}
	return s, c, nil
}

// failure is the service's last call, which must have answered an error.
func failure(sc *core.Scenario, name string) (*service, *call, error) {
	s, c, err := lastCall(sc, name)
	if err != nil {
		return nil, nil, err
	}
	if c.result != nil {
		return nil, nil, secrets.Hide(sc, core.Failf("The %s jsonrpc service answered %s with a result, not an error: %s", s.name, c.method, c.result))
	}
	return s, c, nil
}
