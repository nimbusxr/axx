---
name: axx-acceptance-tests
description: Write and run acceptance tests with axx, black-box against services (REST and OpenAPI, WireMock, SQL, MongoDB, Kafka, logs, files, web apps in real browsers, cloud services). Use when turning acceptance criteria into .feature files, adding scenarios, or editing axx.yaml.
license: Apache-2.0
---

# Writing acceptance tests with axx

axx runs Gherkin scenarios black-box against services built locally.

## Checklist

```
- [ ] axx steps: read every step of the project's packs, a line each, once
      (axx steps show <id>... gives steps' documentation and examples, several at once)
- [ ] Write one scenario per acceptance criterion with steps that exist, written
      out: each {parameter} replaced with a value, a(n) as "a" or "an", row(s) as
      "row" or "rows", [[...]] kept without the brackets or left out
- [ ] axx up: start the apps once; they keep running
- [ ] axx run --compact: fix what it reports (failures, warnings, hints), run again
- [ ] axx down when done
```

With the axx MCP server the loop is the same: `steps_search` without a query (the catalog), `scenarios_run`, `failure_context`, `env`; `steps_try` tries steps in a live scenario before you write them. If a command cannot start, `axx doctor` says why. `axx explain "<line>"` shows how a line is read, and `axx validate` checks features without running them.

## Write criteria for people

- **One behavior per scenario:** Given sets up the state, When is the one thing someone does, Then is what is true afterwards. Never When, Then, When, Then: that is several scenarios.
- Name scenarios after the behavior ("A shop cannot register the same reference twice").
- Use the steps as written; no variables, captured values or step chains. Choose the data up front.
- If the steps cannot say it, ask for a step or write a custom one named after the business action (the `axx-custom-steps` skill).

## Rules that make a scenario prove something

- **Unique data in every scenario.** Scenarios run in parallel and data persists between runs: give every id, key and name a value of the scenario's own (`PX-DUP-0001`, never `test`). The rules under `lint:` in `axx.yaml` enforce it.
- **Check what the service did, not only its status.** A `201` says the request was accepted, not that the parcel was stored; a `400` says it was refused, not why (a missing field refuses it too): check the response property or the problem detail naming the field, the row, the event. A check that something did not happen passes when nothing happened at all: pair it with one that something did.
- **Register services first**, in a `Background`, with the "with the following properties" steps. The first one registered is the default; name others with `... on <service>`.
- **Apps or commands:** what runs on its own and is reached by an address (a service, a database, an MCP server over HTTP) is an app in `axx.yaml`. A program its user starts (a command-line tool, an MCP server its clients run over stdio) is run by its step's `command`, as its user would; it is not an app.
- **Ordinals** (`1st`, `2nd`) count a scenario's requests, responses and selections in the order it adds them, and a step without one means the first, even after later ones: write `the 2nd selection has 1 row` to check the second. The next request after `a GET request to ...` is `a 2nd ordered ... request`.
- **Request payload tables:** a row sets one property; `recipient.name` sets it inside `recipient`, creating the object when the payload lacks it. `"53111"` in double quotes is a string, `null` is JSON null, `undefined` removes the property; single quotes are part of a table value. A first row naming the columns (`| JSONPath | value |`) is optional.
- **A waiting step** (`within 10s a selection of at least 1 row ...`) fails when the rows do not come in time.
- Tag scenarios that must not run in parallel (ones that change database triggers, say) with a tag listed in `run.exclusive`, usually `@isolated`.

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
      | reference      | PX-REG-0001 |
      | recipient.name | Ada Example |
    When the request is executed
    Then the response status code is 201
    And the response payload property status is 'REGISTERED'
```

With an `openapi` URL, requests and responses that break the contract fail the `executed` step.

## More

- Seeds, event payloads and mock bodies are files the steps read. When files of one shape repeat, fixture factories generate them (the `axx-test-data` skill).
- `axx.yaml`: `axx schema --outline` lists its keys; `references/config.md` has a commented example.
- Failures: the `axx-debugging` skill.
- The steps are axx's to tell: `axx steps` lists every step of the project's packs, its own included, a line each, and `axx steps show <id>...` gives their documentation and examples. `references/parameter-types.md` says what `{ordinal}`, `{word}` and the others match.
