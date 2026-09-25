---
name: axx-acceptance-tests
description: Write and run Gherkin acceptance tests with axx, the human-readable acceptance testing framework, black-box against services (REST + OpenAPI, WireMock, SQL, MongoDB, Kafka, logs). Use when turning acceptance criteria into .feature files, adding scenarios, or editing axx.yaml.
license: Apache-2.0
---

# Writing acceptance tests with axx

axx (github.com/nimbusxr/axx, "axxeptance") runs Gherkin scenarios black-box against services built locally.

## The loop (follow it every time)

1. **Check the project.** Run `axx doctor`. Fix anything marked FAIL before writing tests.
2. **One scenario per acceptance criterion.** Name each scenario after the behavior, e.g. "A shop cannot register the same reference twice", not the implementation.
3. **Find real steps. Never invent step text.** For each Given/When/Then you need, run
   `axx steps search "<what you want to do>"`, or read `references/step-index.md`.
   Copy the expression exactly and fill in its `{parameters}`. The search covers the packs in
   `axx-packs.yaml`; `axx pack list` shows axx's other packs, and `axx pack add <name>` adds one.
4. **Validate without running:** run `axx validate`. For any line it flags, `axx explain "<line>"` shows how axx reads it, and `did you mean` suggests the closest real steps. Then run `axx lint` after adding seeds, payloads or fixtures: it reports values (ids, keys) that collide with other files.
5. **Start the apps once:** `axx up`. They keep running between runs. Stop them with `axx down` when you are done.
6. **Run:** `axx run --compact` (or `axx run features/x.feature:LINE` for one scenario).
7. **Diagnose failures:** read the expected/actual values. `axx run --json` adds request/response context. See the `axx-debugging` skill.

## Write criteria for people

A feature file is the acceptance criteria, written so a product owner can read and agree with it. Every scenario should read as a plain statement of behavior.

- Use the steps as written. They were composed to read naturally; don't bend them into code.
- No programming constructs in Gherkin: no variables, captured values, computed data or step chains that pass values around. Choose the data up front (unique ids you seed or send) and assert on outcomes.
- If something can't be said with the available steps, ask for a step or write a custom one named after the business action ("an order is placed for customer 'c-001'"), not a technical one.

## Rules that prevent flaky tests

- **Unique data in every scenario.** Scenarios run in parallel, and seeded rows, published events and mock journals persist between runs. Give every id, name and key a scenario-specific value, e.g. `PX-DUP-0001`, never `test`. `axx lint` enforces this with the rules under `lint:` in `axx.yaml`; each finding gives `file:line` and the colliding value.
- **Assert on observable outcomes.** Check the response status and payload, the rows in a table, the events on a topic, or the requests a mock received. A scenario that passes when the feature is broken is worse than no scenario.
- **Declare the services each scenario needs in a `Background`**, using the "with the following properties" steps. The first service registered is the default one. Name services explicitly (`... on <service>`) when a scenario uses more than one of the same kind.
- **Ordinals** (`1st`, `2nd` ...) address the Nth request or selection within a scenario. Leaving the ordinal out means the first. SQL selections and triggers are numbered in the order they are retrieved: the ordinal in "a 2nd selection of rows is retrieved ..." is only a label, so number retrievals 1st, 2nd, 3rd in order. `axx validate` and `axx lint` warn when an ordinal cannot work.
- **Values in tables:** `'quoted'` forces a string. `null` means JSON null. `undefined` means the property is absent. Unquoted numbers and booleans are typed.
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
- `fixtures`: fixture factories. Payloads, mock bodies and seed datasets are generated from `*.factory.yaml`, `*.fixture.yaml` and `*.prototype.yaml` sources and validated against their schemas (Avro, JSON Schema, OpenAPI, XSD, protobuf, SQL DDL). Edit the sources, never the generated files, then run `axx fixtures generate`; `axx fixtures check` verifies nothing drifted. `axx schema --kind factory|fixture|prototype` prints the file formats, and `axx fixtures adopt` turns existing hand-written files into sources.

## References

- `references/step-index.md`: every available step, one per line. Search it first.
- `references/steps-<pack>.md`: full documentation and examples for each pack.
- `references/parameter-types.md`: what `{ordinal}`, `{word}`, `{string}` and the rest match.
- `references/config.md`: axx.yaml essentials.
