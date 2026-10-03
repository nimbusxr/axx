---
name: axx-acceptance-tests
description: Write and run Gherkin acceptance tests with Axx, the human-readable acceptance testing framework, black-box against services (REST + OpenAPI, WireMock, SQL, MongoDB, Kafka, logs, files, web apps in real browsers, cloud services). Use when turning acceptance criteria into .feature files, adding scenarios, or editing axx.yaml.
license: Apache-2.0
---

# Writing acceptance tests with Axx

Axx (github.com/nimbusxr/axx, "axxeptance") runs Gherkin scenarios black-box against services built locally.

## The loop (follow it every time)

1. **Check the project.** Run `axx doctor`. Fix anything marked FAIL before writing tests. Ask axx for what the project has rather than reading every file: `axx doctor --json` (prerequisites, packs, agents), `axx config show` or the MCP tool `config_show` (the effective `axx.yaml` and its packs), `axx validate --json` (features, scenarios, steps).
2. **One scenario per acceptance criterion.** Name each scenario after the behavior, e.g. "A shop cannot register the same reference twice", not the implementation.
3. **Find real steps. Never invent step text.** For each Given/When/Then you need, run
   `axx steps search "<what you want to do>"`, or read `references/step-index.md`.
   Copy the expression exactly and fill in its `{parameters}`. The search covers the packs in
   `axx-packs.yaml`; `axx pack list` shows Axx's other packs, and `axx pack add <name>` adds one.
4. **Prepare the test data.** Seeds, event payloads and mock bodies are files the steps read. When data repeats across scenarios (several files of one shape that differ in a few fields), use fixture factories: add a `*.fixture.yaml` to the factory that already generates files like it, or turn hand-written ones into a factory with `axx fixtures adopt`, then run `axx fixtures generate`. `axx fixtures generate --dry-run --json` lists the files the factories generate, `axx fixtures explain <file> <path>` says which source sets one of their values, and `axx fixtures adopt --dry-run` previews an adoption. The `axx-test-data` skill shows how. Factories are optional: a one-off file can stay hand-written.
5. **Validate without running:** run `axx validate`. For any line it flags, `axx explain "<line>"` shows how Axx reads it, and `did you mean` suggests the closest real steps. Then run `axx lint` after adding seeds, payloads or fixtures: it reports values (ids, keys) that collide with other files, and hints at a factory when hand-written files repeat one shape.
6. **Start the apps once:** `axx up`. They keep running between runs. Stop them with `axx down` when you are done.
7. **Try steps you are unsure of** in a live scenario, with the `axx mcp` tools: `steps_try` runs steps in a scenario that stays open between calls (a browser on the page they opened, say), and `web_page` shows that page: its elements as steps name them (`the "Get a quote" button`), what a screen reader reads, and a screenshot. Try, look, then write the steps down. `axx run --pause-at features/x.feature:LINE` does the same for a person, in Playwright's Inspector.
8. **Run:** `axx run --compact` (or `axx run features/x.feature:LINE` for one scenario).
9. **Diagnose failures:** read the expected/actual values. `axx run --json` adds request/response context; the MCP tool `failure_context` without a run ID reads the latest `scenarios_run`. See the `axx-debugging` skill.

## Write criteria for people

A feature file is the acceptance criteria, written so a product owner can read and agree with it. Every scenario should read as a plain statement of behavior.

- **One behavior per scenario: Given, then When, then Then.** Given sets up the state (seeds, a registered service, a page already opened, a click that got there), When is the one thing someone does, Then is what is true afterwards; `And` continues the part it follows. Never When, Then, When, Then: that is several scenarios, each needing its own data. A failure then names the behavior that broke.
- Use the steps as written. They were composed to read naturally; don't bend them into code.
- No programming constructs in Gherkin: no variables, captured values, computed data or step chains that pass values around. Choose the data up front (unique ids you seed or send) and assert on outcomes.
- If something can't be said with the available steps, ask for a step or write a custom one named after the business action ("an order is placed for customer 'c-001'"), not a technical one.

## Rules that prevent flaky tests

- **Unique data in every scenario.** Scenarios run in parallel, and seeded rows, published events and mock journals persist between runs. Give every id, name and key a scenario-specific value, e.g. `PX-DUP-0001`, never `test`. `axx lint` enforces this with the rules under `lint:` in `axx.yaml`; each finding gives `file:line` and the colliding value.
- **Assert on observable outcomes.** Check the response status and payload, the rows in a table, the events on a topic, or the requests a mock received. A scenario that passes when the feature is broken is worse than no scenario.
  - A status code alone rarely proves the behavior: a `201` says the request was accepted, not that the parcel was stored or announced. Check what the service did too.
  - `axx validate`, `axx lint` and a passing `axx run` print a `hint:` for a scenario that checks only a success status, or only that something did not happen. Each is a finding to judge: add the check that proves the behavior, never weaken or delete the scenario to silence it.
  - They also print one for a scenario that checks several things in turn (When … Then …, then When … Then … again): give each acceptance criterion its own scenario.
  - A check that something did not happen (a mocked request not received, no rows) passes when the service did nothing at all. Pair it with a check that the action reached that point: the `400` and its problem detail that show the service refused the parcel, next to the address check it never made.
- **Declare the services each scenario needs in a `Background`**, using the "with the following properties" steps. The first service registered is the default one. Name services explicitly (`... on <service>`) when a scenario uses more than one of the same kind.
- **Ordinals** (`1st`, `2nd` ...) address the Nth request or selection within a scenario. Leaving the ordinal out means the first. SQL selections and triggers are numbered in the order they are retrieved (a `within 10s a selection of at least 1 row ...` step fails when the rows do not come in time): the ordinal in "a 2nd selection of rows is retrieved ..." is only a label, so number retrievals 1st, 2nd, 3rd in order. A service's REST requests are numbered in the order they are added: `a GET request to ...` is the 1st, and the next is `a 2nd ordered ... request`. `axx validate` and `axx lint` warn when an ordinal cannot work, and show the step to write.
- **Values in a request payload table**: JSON bare (`{"name": "Ada"}`), strings bare or in double quotes. Single quotes are part of a table value (unlike in a step's own text), and `axx validate` warns about them.
- **Tables are their rows.** Each row of a step's table is data, like `| reference | PX-4101 |`. A first row naming the columns, as `axx steps show` lists them (`| JSONPath | value |`), is optional.
- **Values in tables:** `"double quotes"` force a string (`"53111"`). `null` means JSON null. `undefined` means the property is absent. Unquoted numbers and booleans are typed, except that a request payload value keeps the type of the property it replaces.
- **Tag scenarios** that must not run in parallel (for example, ones that change database triggers) with a tag listed in `run.exclusive` (usually `@isolated`).

## Shape of a feature

```gherkin
Feature: Register parcels
  Shops register every parcel before they hand it to the depot.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |

  Scenario: A shop registers a parcel
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference | PX-REG-0001 |
    When the request is executed
    Then the response status code is 201
    And the response payload property status is 'REGISTERED'
```

OpenAPI validation is on whenever a service has an `openapi` URL. Requests and responses that violate the spec fail the `When ... executed` step.

## Configuration

`axx.yaml` sits at the project root. Run `axx schema` for the full JSON Schema; the commented example is in `references/config.md`. Its main sections:

- `apps`: how to start and stop the system under test (`command`, `ready`, `cleanup`).
- `run.paths`: where the features are.
- `properties`: values for `${sys:name}`.
- `packs`: the packs' settings, by pack name, such as `packs.web-core.traces`. `axx run --set packs.<pack>.<key>=value` sets one for a run.
- `fixtures`: fixture factories. Payloads, mock bodies and seed datasets are generated from `*.factory.yaml`, `*.fixture.yaml` and `*.prototype.yaml` sources and validated against their schemas (Avro, JSON Schema, OpenAPI, XSD, protobuf, SQL DDL). Edit the sources, never the generated files, then run `axx fixtures generate`; `axx fixtures check` verifies nothing drifted. The `axx-test-data` skill covers them.

## References

- `references/step-index.md`: every available step, one per line. Search it first.
- `references/steps-<pack>.md`: full documentation and examples for each pack.
- `references/parameter-types.md`: what `{ordinal}`, `{word}`, `{string}` and the rest match.
- `references/config.md`: axx.yaml essentials.
