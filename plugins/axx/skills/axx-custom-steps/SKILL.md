---
name: axx-custom-steps
description: Add custom Gherkin steps to axx (the human-readable acceptance testing framework) as Go packs that share the context of axx's packs. Use when no step of axx's packs covers what a scenario needs (check `axx steps search` and `axx pack list` first).
license: Apache-2.0
---

# Custom steps for axx

Check the existing steps first: `axx steps search "<intent>"` searches the packs in `axx-packs.yaml`, and `axx pack list` shows axx's other packs (`axx pack add <name>` adds one).

## Packs (Go)

`axx pack new ./steps` creates a pack and adds it to `axx-packs.yaml`. A pack is a Go package exporting `func Pack() core.Pack` (`github.com/nimbusxr/axx/core`); its `Manifest()` lists steps (`core.StepDef{ID, Keyword, Expr, Doc, Examples, Run}`).

- Steps get the scenario and every pack's context: `rest.Context(sc)`, `sql.Context(sc)`, `mongo.Context(sc)`, `mock.Context(sc)` (packages under `github.com/nimbusxr/axx/packs/`). `Service(name...)` returns a registered service (no name: the default), with its state: REST requests and responses, SQL connection and selections, MongoDB database and selections.
- Parameter types of loaded packs (`{service}`, `{dbService}`, `{ordinal}`, `{duration}`...) work in custom expressions; `a.Value(i)` returns the resolved value.
- Report failed expectations with `core.Fail(message, expected, actual)`. Keep your own per-scenario state in `core.NewStateKey`.
- The first command that needs the project's steps prepares axx with the pack, once; later runs reuse it until the pack changes.
