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
