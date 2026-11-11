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

Once the pack is listed in `axx-packs.yaml`, its steps are used like any other step: `axx steps` lists them, `axx steps search` finds them, and `axx validate` checks feature lines against them. `axx docs export --out <dir>` writes the reference of the project's packs, yours included: a page per pack, with its steps, their documentation and examples.

## Read a table's values

A table of values follows [the rule every pack follows](/references/steps/#values): `42` is an integer, `"42"` the string, and an empty cell an error that says what to write. Say what its cells are in the step's `Table`, and `axx validate` and the editors report the cells the rule cannot read:

```go title="steps/pack.go"
Table: &core.TableDoc{
	Columns:   []string{"JSONPath", "value"},
	Values:    core.JSONValues, // core.TextValues where only text fits
	Undefined: true,            // the cells take `undefined`
},
```

In the step, read each cell with `core.ParseValue`, or `core.ParseText` where only text fits. Fail the step with the error either returns:

```go
pairs, err := a.Table.Pairs()
if err != nil {
	return err
}
for _, p := range pairs {
	v, err := core.ParseValue(p.Value, p.Null, true)
	if err != nil {
		return fmt.Errorf("%s: %w", p.Key, err)
	}
	// v.Str() gives a string, v.JSON() any value as JSON; v.Undefined() and v.IsNull() say which it is.
}
```

A check of your own compares by the same rule with `v.Is`, given what the source holds as JSON. Types must match, so `2` is no decimal `2.0`, and `"42"` no number:

```go
if !v.Is(actualJSON) {
	return core.Fail("the carrier code differs", p.Value, actualJSON)
}
```

A `{string}` the step sets something to or checks something against is a value too, read by the same rule. Say so, by the argument's place, and `axx validate` and the run report what the rule cannot read (`'5'` asks for `'"5"'`):

```go title="steps/pack.go"
Expr:     "the {word} carrier code is {string}",
TextArgs: []int{1}, // a.String(1) is the text; TypedArgs for any type, which the step reads with core.ParseValue
```

A name that finds something, or a pattern, is not a value: leave it out.

## Change a REST request

A custom step can change a request the rest pack's steps built, until it is executed: sign it, or give it a token the service expects. A request's headers are the ones it is sent with:

- `Header()` returns them, and `Set`, `Add` and `Del` change them, as with Go's `http.Request.Header`.
- `SetHeader(name, value)` does what the request header steps do: `Content-Type` and `Accept` replace an earlier value, and other headers get one more.
- When the request is sent, it gets `Accept: */*`, its payload's `Content-Type` and a `User-Agent` if it has none. `Exchange().RequestHeader` has the headers it was sent with, and `Exchange().Header` the response's.

This step signs a request's payload for a service that checks an `X-Signature` header, like a carrier's scan webhook:

```go title="steps/pack.go"
{
	ID:       "steps.sign",
	Keyword:  "Given",
	Expr:     "the request is signed with the key {string}",
	Doc:      "Signs the default REST service's request: an X-Signature header with the HMAC-SHA256 of its payload.",
	Examples: []string{"Given the request is signed with the key '${env:CARRIER_KEY}'"},
	Run: func(sc *core.Scenario, a core.Args) error {
		svc, err := rest.Context(sc).Service()
		if err != nil {
			return err
		}
		req, err := svc.Request(0)
		if err != nil {
			return err
		}
		if req.Payload == nil {
			return errors.New("the request has no payload to sign")
		}
		mac := hmac.New(sha256.New, []byte(a.String(0)))
		mac.Write([]byte(*req.Payload))
		req.Header().Set("X-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		return nil
	},
},
```

```gherkin nocheck
Scenario: A carrier's signed scan is accepted
  Given a POST request to /api/scans
  And a request payload using an application/json content example
  And the request is signed with the key '${env:CARRIER_KEY}'
  When the request is executed
  Then the response status code is 202
```

The step signs the payload as the steps before it left it, so it comes after the payload steps.

## Make data from fixtures

A pack that makes data of its own, a stream of records, say, can make it from the project's [fixture factories](/guides/fixture-factories/). The `fixtures` package gives it one fixture's values for an instance it names, as `axx fixtures check` resolves them, without writing a file:

```go
lib, err := fixtures.Load(fixtures.DefaultConfig("data"))
if err != nil {
	return err
}
parcel, err := lib.Fixture("parcels/parcels#standard")
if err != nil {
	return err
}
for i := range count {
	scope := fixtures.InstanceScope{Discriminator: fmt.Sprintf("%s|%d", runID, i)}
	instance, err := lib.Instance(parcel, fixtures.Input{Scope: scope})
	if err != nil {
		return err
	}
	send(instance.Data.CompactJSON(), instance.Identities["reference"])
}
```

The same fixture and scope give the same bytes. An identity the data has no value for derives one from the scope, so give each instance a discriminator of its own: `Instance` claims no identity, and keeping them unique is the pack's to do. `Input.Overrides` sets values, `Input.Documents` resolves `$ref`s to other documents, and a reference that names no fixture fails with `AXX-E0911`.

## Produce to a Kafka service

Steps that publish in a way of their own can still reach a Kafka service the scenario declared as the publish steps do, with its connection, its security and its serializers:

```go
svc, err := kafka.Context(sc).Service()
if err != nil {
	return err
}
client, err := svc.Producer(sc, "depot-scans")
if err != nil {
	return err
}
serializer, err := svc.Serializer(sc, "depot-scans")
if err != nil {
	return err
}
value, err := serializer.AvroJSON(ctx, schema, payload)
if err != nil {
	return err
}
client.Produce(ctx, &kgo.Record{Topic: "depot-scans", Value: value}, nil)
```

`Producer` is the client the publish steps use: the run closes it, so don't. To produce at a rate it isn't tuned for, make a client of your own from `ProducerOpts`, and close that one. `Serializer` writes values as the topic's producer does, with its Schema Registry and the Confluent wire format.

## Tell steps apart in a hook

A hook around a step can ask which step it is: `sc.Step()` gives its text and line, and once it matched, `Definition`, the `ID` of the step it matched, and `Pack`, the pack that registered it. Those stay the same however the feature words the step, so a hook can act on what a step is:

```go
Hooks: []core.Hook{{
	ID:    "steps.wait-before-publishing",
	Phase: core.BeforeStep,
	Run: func(sc *core.Scenario) error {
		if step := sc.Step(); step != nil && step.Pack == "kafka" {
			return waitForTheConsumers(sc)
		}
		return nil
	},
}},
```

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

## Run something before or after a command

A pack can run code before or after one of axx's commands, or a command a pack adds: `CommandHooks` in its manifest. This one writes the carriers' rate card the fixtures read, from the rates service's export, before `axx fixtures generate`:

```go
CommandHooks: []core.CommandHook{{
	Command: "fixtures generate",
	Phase:   core.BeforeCommand,
	Doc:     "Writes the carriers' rate card the fixtures read, from the rates service's export.",
	Run: func(call *core.CommandHookCall) error {
		var s struct {
			RateCard string `json:"rateCard"`
		}
		if err := call.Settings(&s); err != nil || s.RateCard == "" {
			return err
		}
		fmt.Fprintf(call.Stderr, "writing %s\n", s.RateCard)
		return exportRateCard(call.Context, filepath.Join(call.ProjectDir, s.RateCard))
	},
}},
```

- **Before or after, never inside.** A before hook that fails stops the command, and its after hooks don't run. An after hook runs whether the command succeeded or failed (`call.Err` says which); when it fails, a command that succeeded fails too.
- **The hook is told what the command was given:** `call.Args`, `call.Flags` (`dry-run`, say), and `call.Settings`, the pack's section of axx.yaml, read with the command's `--profile` and `--set`. It writes to `call.Stderr`: stdout is the command's, its `--json` answer included.
- **Several packs' hooks of one command** run before it in the order the packs load, and after it in the reverse order.
- **Hooks run in a project that uses the pack,** on every command but those that manage axx and its packs (`pack`, `init`, `upgrade`, `install` and `version`) and `fmt`, which only lays out feature files. They run around the project's own hooks, those of `axx.yaml` ([Run your commands around axx's](/guides/run-hooks/)). `axx doctor` lists a project's hooks, and any that never run.

## Check what the pack needs

`axx doctor` runs a pack's `Checks` in the projects that use it: that what its steps need is there, before a scenario finds out. The mobile packs check the Android SDK and Xcode this way.

```go
Checks: []core.Check{{
	Name: "carrier signing key",
	Run: func(ctx context.Context) core.CheckResult {
		if os.Getenv("CARRIER_KEY") == "" {
			return core.CheckResult{Status: core.CheckWarn, Detail: "CARRIER_KEY is not set", Hint: "set it to the key carriers sign their scans with"}
		}
		return core.CheckResult{Status: core.CheckOK, Detail: "CARRIER_KEY is set"}
	},
}},
```

A check says what it found (`Detail`), and, when something is wrong, what puts it right (`Hint`). `CheckWarn` is for what only some scenarios need; `CheckFail` for what none can run without.
