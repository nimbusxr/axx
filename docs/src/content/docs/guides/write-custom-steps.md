---
title: Write custom steps
description: Add your own Gherkin steps as a Go pack that shares the context of Axx's packs.
---

Custom steps sit next to the steps of Axx's packs and are used the same way in feature files. They live in a **pack** of your own: Go code that builds on Axx's core, like Axx's packs, and works on the same context their steps use.

## Create a pack

Create a pack and add it to the project in one go:

```console
$ axx pack new ./steps
created the steps pack in ./steps and added it to axx-packs.yaml
its example step: Then the steps pack is loaded
```

A pack is a Go package that exports `Pack()`. Its steps receive the scenario, and from it every pack's context: the services registered in the scenario and their state. A custom step sees exactly what the steps of Axx's packs set up, and their steps see what it adds.

```go title="steps/pack.go"
package steps

import (
	"github.com/nimbusxr/axx/core"
	"github.com/nimbusxr/axx/packs/sql"
)

func Pack() core.Pack { return pack{} }

type pack struct{}

func (pack) Manifest() core.Manifest {
	return core.Manifest{
		Name: "steps",
		Steps: []core.StepDef{{
			ID:       "steps.rows",
			Keyword:  "Then",
			Expr:     "the {word} table on {dbService} holds {int} row(s)",
			Doc:      "Counts the rows of a table on a registered database.",
			Examples: []string{"Then the parcels.manifest_lines table on parcels-db holds 3 rows"},
			Run: func(sc *core.Scenario, a core.Args) error {
				db := a.Value(1).(*sql.Service) // {dbService} resolves to the registered service
				sel, err := db.Query(sc.Context(), a.String(0), "SELECT * FROM "+a.String(0))
				if err != nil {
					return err
				}
				if got := len(sel.Rows); got != a.Int(2) {
					return core.Fail("rows in "+a.String(0), a.Int(2), got)
				}
				return nil
			},
		}},
	}
}
```

The contexts:

| Pack | Context | Holds |
|---|---|---|
| `rest` | `rest.Context(sc)` | REST services, their requests in order, and each request's response (`Exchange()`) |
| `sql` | `sql.Context(sc)` | databases with their connection pool (`DB`), selections, triggers; `Query`, `AddSelection` |
| `mongo` | `mongo.Context(sc)` | MongoDB databases (`DB()`) and their selections |
| `mock` | `mock.Context(sc)` | mocked (WireMock) services |
| `kafka` | `kafka.Context(sc)` | Kafka services, their topic clients (`Topic(name)`) and the events drafted for each topic |

Every context has `Service(name...)` (no name: the default, the first registered), `Services()` and `AddService(...)`, and `sql.Connect`, `mongo.Connect` and `mock.NewService` create services the way the packs' own steps do. Report failed expectations with `core.Fail(message, expected, actual)` so reports show both values, and keep state of your own in a `core.NewStateKey`: it is per scenario, like the packs' contexts.

A step parameter that names a file is a `{filepath}`, and a step whose `key | value` table has file properties names them in `TableTypes`: `map[string]string{"schema": "filepath"}` for a file the step reads, or `"url"` for a URL whose `file://` form names a file that may appear only during the run. Read the file with `sc.Suite().ResolvePath(a.String(0))`, which looks in the `resources` directories and next to `axx.yaml`: that is where editors look to link the value, complete its path and flag a missing file ([Set up your editor](/guides/set-up-your-editor/)).

The first `axx` command that needs the project's steps prepares Axx with the pack, like any other pack in `axx-packs.yaml` ([Choose packs](/guides/use-packs/)).

## Use the step

Once the pack is listed in `axx-packs.yaml`, its steps are used like any other step: `axx steps search` finds them, `axx validate` checks feature lines against them, and `axx skills install` adds them to the agent step index.

## Change a REST request

A custom step can change a request the rest pack's steps built, until it is executed: sign it, or give it a session or a token from an earlier response. A request's headers are the ones it is sent with:

- `Header()` returns them, and `Set`, `Add` and `Del` change them, as with Go's `http.Request.Header`.
- `SetHeader(name, value)` does what the request header steps do: `Content-Type` and `Accept` replace an earlier value, and other headers get one more.
- When the request is sent, it gets `Accept: */*`, its payload's `Content-Type` and a `User-Agent` if it has none. `Exchange().RequestHeader` has the headers it was sent with, and `Exchange().Header` the response's.

This step sends the cookies an earlier response set with a later request:

```go title="steps/pack.go"
{
	ID:       "steps.session",
	Keyword:  "Given",
	Expr:     "the {ordinal} ordered request sends the session of the {ordinal} ordered response",
	Doc:      "Sends the cookies a response of the default REST service set with a later request.",
	Examples: []string{"Given the 2nd ordered request sends the session of the 1st ordered response"},
	Run: func(sc *core.Scenario, a core.Args) error {
		svc, err := rest.Context(sc).Service()
		if err != nil {
			return err
		}
		to, err := svc.Request(a.Int(0) - 1)
		if err != nil {
			return err
		}
		from, err := svc.Request(a.Int(1) - 1)
		if err != nil {
			return err
		}
		if from.Exchange() == nil {
			return errors.New("the request of that response is not executed yet")
		}
		var cookies []string
		for _, line := range from.Exchange().Header.Values("Set-Cookie") {
			if c, err := http.ParseSetCookie(line); err == nil {
				cookies = append(cookies, c.Name+"="+c.Value)
			}
		}
		if len(cookies) == 0 {
			return errors.New("the response set no cookie")
		}
		to.Header().Set("Cookie", strings.Join(cookies, "; "))
		return nil
	},
},
```

```gherkin nocheck
Scenario: A signed-in shop sees its parcels
  Given a POST request to /api/sessions
  And a request payload using an application/json content example
  And a 2nd ordered GET request to /api/parcels?sender=kestrel-books
  When the request is executed
  And the 2nd ordered request sends the session of the 1st ordered response
  And the 2nd ordered request is executed
  Then the 2nd ordered response status code is 200
```

The step reads the session from the scenario's own responses, so a session never reaches another scenario. A response is the one at the end of the redirects its request followed: a cookie that a followed `303 See Other` set is not in it ([Send REST requests](/guides/send-rest-requests/#redirects)).

## Give agents a tool

A pack can also give coding agents tools of their own, in `axx mcp`: `Tools` in its manifest. A tool looks at the scenario an agent keeps open with `steps_try`, the one its steps ran in, and returns data (and images) for the agent. The web-core pack's `web_page` shows the page the steps led to ([Set up agents](/guides/set-up-agents/#mcp-server)).

```go
Tools: []core.Tool{{
	Name:        "steps_manifest_lines",
	Description: "The manifest lines the session's steps imported, as the importer stored them.",
	ReadOnly:    true,
	Input:       json.RawMessage(`{"type": "object", "properties": {}}`),
	Run: func(call *core.ToolCall) (*core.ToolResult, error) {
		db, err := sql.Context(call.Scenario).Service()
		if err != nil {
			return nil, err
		}
		sel, err := db.Query(call.Context, "manifest_lines", "SELECT * FROM parcels.manifest_lines")
		if err != nil {
			return nil, err
		}
		return &core.ToolResult{Data: map[string]any{"rows": sel.Rows}}, nil
	},
}},
```

Name a tool after its pack, and describe what it gives and when to use it: agents choose tools by their descriptions. The input is checked against its JSON Schema before `Run`.
