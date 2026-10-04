package mcp

import (
	"encoding/json"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/internal/jsonassert"
	"github.com/nimbusxr/axx/internal/secrets"
)

const argumentsNote = "Each row is an argument, or a path into one (`address.postcode`), and its value: a value takes the type the tool's input schema " +
	"gives its argument, double quotes make it text, `null` is null and `undefined` leaves it out."

const levelsNote = "A row is a validation key and its level: `ERROR` (or `FAIL`), `WARN`, `INFO` or `IGNORE`. " +
	"The key must be one the pack reports, or a prefix of such keys, like `validation.arguments`: " +
	"any other fails the step, which names the closest keys."

const resultNote = "The result is the tool's latest in the scenario, on the server the step names, or on the only server the scenario called the tool on."

func steps() []core.StepDef {
	return []core.StepDef{
		{
			ID: Name + ".server", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr: "the {word} mcp server with the following properties:",
			Doc: "Register an MCP server under a name, and connect to it: over stdio (a `command` the scenario runs, and ends when it ends) " +
				"or over streamable HTTP (a `url`). The session agrees on the protocol version with the server.\n\n" +
				"- A server its clients start over stdio is the scenario's `command`, as assistants run it, not an app in axx.yaml; " +
				"a server that runs on its own over HTTP is an app, which the `url` reaches.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Rows: []core.TableRow{
					{Name: "command", Takes: "the command that runs the server over stdio, split into words without a shell, like `docker compose exec -T app parcels mcp`"},
					{Name: "url", Takes: "the server's streamable HTTP endpoint"},
					{Name: "dir", Takes: "the folder the command runs in, relative to the project", Default: "a folder of the scenario's own"},
					{Name: "env.<NAME>", Takes: "an environment variable of the command, such as `env.PARCELS_DB_URL`"},
					{Name: "header.<name>", Takes: "a header sent with every request to the url, such as `header.Authorization`"},
					{Name: "timeout", Takes: "how long connecting, or a call, may take, like `10s`", Default: "30s"},
					{Name: "protocol version", Takes: "the MCP version to speak, to check that the server still answers an earlier one, like `2025-06-18`", Default: "the latest the server speaks"},
				},
				Note: "A `command` or a `url` is required. Values are expanded (`${env:..}`, `${sys:..}`, `${token:..}`); what `${env:..}` references expand to is masked everywhere.",
			},
			Examples: []string{
				"Given the parcels mcp server with the following properties:\n" +
					"  | url | http://localhost:8400/mcp |",
				"Given the parcels mcp server with the following properties:\n" +
					"  | command | ${sys:parcels.mcp} |\n" +
					"  | dir     | ../infra           |",
			},
			TableTypes: map[string]string{"dir": "filepath"},
			Run:        register,
		},
		{
			ID: Name + ".tool", Keyword: "Then", Arg: core.ArgOptional, Since: since,
			Expr: "the {word} mcp server has the {word} tool[[ with the following properties:]]",
			Doc: "Check that the server lists the tool, and that it has the table's properties: what an AI assistant reads before it calls " +
				"the tool, like its description and its hints.",
			Table: &core.TableDoc{
				Columns: []string{"property", "value"},
				Note:    "A row is a path into the tool as the server lists it (`description`, `annotations.readOnlyHint`, `inputSchema.required[0]`) and its value.",
			},
			Examples: []string{
				"Then the parcels mcp server has the track_parcel tool",
				"Then the parcels mcp server has the track_parcel tool with the following properties:\n" +
					"  | title                    | Track a parcel |\n" +
					"  | annotations.readOnlyHint | true           |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				s, err := get(sc, a.String(0))
				if err != nil {
					return err
				}
				t, err := s.tool(sc, a.String(1))
				if err != nil || a.Table == nil {
					return err
				}
				b, err := json.Marshal(t)
				if err != nil {
					return err
				}
				return jsonassert.Properties(string(b), a.Table, false)
			},
		},
		{
			ID: Name + ".noTool", Keyword: "Then", Since: since, Absence: true,
			Expr:     "the {word} mcp server does not have the {word} tool",
			Doc:      "Check that the server does not list the tool: one it must not offer an AI assistant.",
			Examples: []string{"Then the parcels mcp server does not have the cancel_parcel tool"},
			Run: func(sc *core.Scenario, a core.Args) error {
				s, err := get(sc, a.String(0))
				if err != nil {
					return err
				}
				tools, err := s.tools(sc)
				if err != nil {
					return err
				}
				for _, t := range tools {
					if t.Name == a.String(1) {
						return core.Failf("the %s mcp server has the %s tool: %s", s.name, t.Name, t.Description)
					}
				}
				return nil
			},
		},
		{
			ID: Name + ".call", Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr: "the {word} tool is called on the {word} mcp server[[ with the following arguments:]]",
			Doc: "Call the tool, with the table's arguments when the step has one. The arguments are checked against the tool's input schema " +
				"before they are sent, and its structured result against its output schema.\n\n" +
				"- A result that is an error does not fail the step: check it with `the {word} tool's result is an error`.\n" +
				"- Nor does a call the server refuses: check it with `the {word} tool's call failed with the error code {int}`.",
			Table: &core.TableDoc{Columns: []string{"path", "value"}, Note: argumentsNote},
			Examples: []string{
				"When the track_parcel tool is called on the parcels mcp server with the following arguments:\n" +
					"  | reference | PX-MCP-9101 |",
			},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callTool(sc, a.String(0), a.String(1), a.Table, "")
			},
		},
		{
			ID: Name + ".call.file", Keyword: "When", Since: since,
			Expr: "the {word} tool is called on the {word} mcp server with the {filepath} arguments",
			Doc: "Call the tool with the arguments of a file of the project: a JSON or YAML object of the arguments' names and values. " +
				"They are checked against the tool's input schema before they are sent.",
			Examples: []string{"When the hold_parcel tool is called on the parcels mcp server with the mcp/hold-parcel.json arguments"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return callTool(sc, a.String(0), a.String(1), nil, a.String(2))
			},
		},
		{
			ID: Name + ".notError", Keyword: "Then", Since: since,
			Expr:     "the {word} tool's result[[ on the {word} mcp server]] is not an error",
			Doc:      "Check that the tool answered, with a result that is not an error.\n\n" + resultNote,
			Examples: []string{"Then the track_parcel tool's result is not an error"},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, r, err := result(sc, a)
				if err != nil {
					return err
				}
				if r.IsError {
					return secrets.Hide(sc, core.Failf("the %s tool's result is an error: %s", c.tool, text(r)))
				}
				return nil
			},
		},
		{
			ID: Name + ".error", Keyword: "Then", Since: since,
			Expr: "the {word} tool's result[[ on the {word} mcp server]] is an error",
			Doc: "Check that the tool answered with a result that is an error: the tool ran, and says what went wrong, for the assistant " +
				"to tell the user or try again. Check what it says with `contains`.\n\n" + resultNote,
			Examples: []string{"Then the hold_parcel tool's result is an error"},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, r, err := result(sc, a)
				if err != nil {
					return err
				}
				if !r.IsError {
					return secrets.Hide(sc, core.Failf("the %s tool's result is not an error: %s", c.tool, text(r)))
				}
				return nil
			},
		},
		{
			ID: Name + ".contains", Keyword: "Then", Since: since,
			Expr: "the {word} tool's result[[ on the {word} mcp server]] contains {string}",
			Doc: "Check that the tool's result has a text: in its content (text, and embedded resources' text), or in its structured " +
				"content's JSON.\n\n" + resultNote,
			Examples: []string{"Then the hold_parcel tool's result contains 'already out for delivery'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, r, err := result(sc, a)
				if err != nil {
					return err
				}
				want := a.String(2)
				if got := text(r); !strings.Contains(got, want) {
					return secrets.Hide(sc, core.Fail("the "+c.tool+" tool's result does not contain the text", want, got))
				}
				return nil
			},
		},
		{
			ID: Name + ".properties", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr: "the {word} tool's result[[ on the {word} mcp server]] has the following properties:",
			Doc: "Check the tool's structured result: its structured content, or the JSON of its text for a tool that answers JSON as " +
				"text.\n\n- Paths are dotted (`lastScan.location`) or JSONPath; `null` and `undefined` work as in the other property steps.\n- " + resultNote,
			Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A row is a path into the structured result, and its value."},
			Examples: []string{"Then the track_parcel tool's result has the following properties:\n" +
				"  | status       | OUT_FOR_DELIVERY |\n" +
				"  | lastLocation | Leipzig          |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, r, err := result(sc, a)
				if err != nil {
					return err
				}
				doc, err := structured(c, r)
				if err != nil {
					return err
				}
				return secrets.Hide(sc, jsonassert.Properties(doc, a.Table, false))
			},
		},
		{
			ID: Name + ".refused", Keyword: "Then", Since: since,
			Expr: "the {word} tool's call[[ on the {word} mcp server]] failed with the error code {int}",
			Doc: "Check that the server refused the call, with a JSON-RPC error of that code: `-32602` for a tool it does not have or invalid " +
				"parameters, `-32603` for an internal error.\n\n" + resultNote,
			Examples: []string{"Then the cancel_parcel tool's call failed with the error code -32602"},
			Run: func(sc *core.Scenario, a core.Args) error {
				c, err := lastCall(sc, a.String(0), a.String(1))
				if err != nil {
					return err
				}
				want := a.Int(2)
				if c.err == nil {
					b, _ := json.Marshal(c.result)
					return secrets.Hide(sc, core.Fail("the "+c.tool+" tool's call did not fail: it answered "+string(b), want, "a result"))
				}
				if int(c.code) != want {
					return core.Fail("the "+c.tool+" tool's call failed with another error code: "+message(c.err), want, int(c.code))
				}
				return nil
			},
		},
		{
			ID: Name + ".resource.read", Keyword: "When", Since: since,
			Expr:     "the {word} resource is read from the {word} mcp server",
			Doc:      "Read a resource of the server, by its URI.",
			Examples: []string{"When the parcels://PX-MCP-9103/label resource is read from the parcels mcp server"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return readResource(sc, a.String(0), a.String(1))
			},
		},
		{
			ID: Name + ".resource.contains", Keyword: "Then", Since: since,
			Expr:     "the {word} resource contains {string}",
			Doc:      "Check that the text of the resource, as the scenario last read it, has a text.",
			Examples: []string{"Then the parcels://PX-MCP-9103/label resource contains 'PX-MCP-9103'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				r, err := lastReading(sc, a.String(0))
				if err != nil {
					return err
				}
				if got := r.text(); !strings.Contains(got, a.String(1)) {
					return secrets.Hide(sc, core.Fail("the resource "+r.uri+" does not contain the text", a.String(1), got))
				}
				return nil
			},
		},
		{
			ID: Name + ".resource.properties", Keyword: "Then", Arg: core.ArgTable, Since: since,
			Expr:  "the {word} resource has the following properties:",
			Doc:   "Check the resource, as the scenario last read it, when its text is JSON.",
			Table: &core.TableDoc{Columns: []string{"property", "value"}, Note: "A row is a path into the resource's JSON, and its value."},
			Examples: []string{"Then the parcels://PX-MCP-9103/label resource has the following properties:\n" +
				"  | reference | PX-MCP-9103 |\n" +
				"  | service   | EXPRESS     |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				r, err := lastReading(sc, a.String(0))
				if err != nil {
					return err
				}
				return secrets.Hide(sc, jsonassert.Properties(r.text(), a.Table, false))
			},
		},
		{
			ID: Name + ".prompt", Keyword: "When", Arg: core.ArgOptional, Since: since,
			Expr:  "the {word} prompt is requested from the {word} mcp server[[ with the following arguments:]]",
			Doc:   "Request a prompt of the server, with the table's arguments when the step has one: the messages it offers an AI assistant.",
			Table: &core.TableDoc{Columns: []string{"argument", "value"}, Note: "A row is an argument of the prompt, and its value, as text."},
			Examples: []string{"When the delivery_update prompt is requested from the parcels mcp server with the following arguments:\n" +
				"  | reference | PX-MCP-9104 |"},
			Run: func(sc *core.Scenario, a core.Args) error {
				return requestPrompt(sc, a.String(0), a.String(1), a.Table)
			},
		},
		{
			ID: Name + ".prompt.contains", Keyword: "Then", Since: since,
			Expr:     "the {word} prompt contains {string}",
			Doc:      "Check that the text of the prompt's messages, as the scenario last requested it, has a text.",
			Examples: []string{"Then the delivery_update prompt contains 'PX-MCP-9104'"},
			Run: func(sc *core.Scenario, a core.Args) error {
				p, err := lastPrompt(sc, a.String(0))
				if err != nil {
					return err
				}
				if got := p.text(); !strings.Contains(got, a.String(1)) {
					return secrets.Hide(sc, core.Fail("the "+p.name+" prompt does not contain the text", a.String(1), got))
				}
				return nil
			},
		},
		{
			ID: Name + ".levels", Keyword: "Given", Arg: core.ArgTable, Since: since,
			Expr:  "the MCP validation levels are:",
			Doc:   "Relax, for this scenario, the checks of tools' arguments and results against their schemas.",
			Table: &core.TableDoc{Columns: []string{"validation key", "level"}, Note: levelsNote},
			Examples: []string{"Given the MCP validation levels are:\n" +
				"  | validation.arguments.schema.required | IGNORE |"},
			Run: setLevels,
		},
	}
}

// result is the latest result of the step's tool, which the server did not
// refuse.
func result(sc *core.Scenario, a core.Args) (*call, *sdk.CallToolResult, error) {
	c, err := lastCall(sc, a.String(0), a.String(1))
	if err != nil {
		return nil, nil, err
	}
	r, err := c.answered()
	return c, r, err
}
