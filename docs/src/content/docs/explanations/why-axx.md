---
title: Why Axx
description: Axx is a human-readable acceptance testing framework for the agentic era - acceptance criteria that people can read and coding agents can write, run against your real service.
---

Axx is a human-readable acceptance testing framework for the agentic era.

Coding agents now write a growing share of code and tests. That makes one question matter more: what exactly was checked? With Axx, the answer is the acceptance criteria themselves. Each scenario states a behavior in plain language, so the people who own that behavior can read and review it. Agents can find the steps, write the scenario, run it and fix what fails.

Axx runs every scenario against your service from the outside, through the same interfaces your users and neighboring services use: its HTTP API, its database, its event streams and the dependencies it calls. When a scenario passes, the behavior it describes works in the running system, not in a mock of it or in one unit on its own.

## One scenario covers the whole flow

```gherkin
Feature: Register parcels

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked addresses service with the following properties:
      | url | http://localhost:8081 |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |

  Scenario: A shop registers a parcel
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-REG-1001 |
      | recipient.postcode | "53111"     |
    When the request is executed
    Then the response status code is 201
    And the mocked GET request to /v1/postcodes/DE/53111 named postcode-check was received by addresses
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-REG-1001 |
    And the 1st row details property for the selection json properties are:
      | source | api  |
      | zone   | DE-1 |
```

This one scenario:
- builds a request from the service's own OpenAPI example and sends it;
- checks the request and the response against that OpenAPI document;
- proves the service asked its downstream address service about the postcode;
- checks what the service stored in PostgreSQL, down to a JSON column.

Every line is a step from one of Axx's packs, so there is no step code to write.

## Test the service, not the code

Axx never loads your code. The service can be written in any language, and the tests keep passing through refactors and rewrites because they only depend on what the service does. The people who wrote the acceptance criteria can read them too. See [Black-box testing](/explanations/black-box-testing/).

These are built in:

| Area | What you get |
|---|---|
| REST | Requests and responses validated against your OpenAPI document |
| Mocked dependencies | Calls to them verified through WireMock |
| SQL | PostgreSQL, MySQL, SQLite and SQL Server: seeds, selections, JSON columns, locks and triggers |
| MongoDB | Seeding and querying |
| Kafka | Publishing and consuming events, with Avro and Schema Registry |

## One binary with nothing to wire up

Axx needs no JVM, no build plugin and no test-runner glue. `axx run` does the whole job:
1. It starts the apps listed in `axx.yaml` and waits until they are healthy.
2. It runs the scenarios in parallel.
3. It stops everything and cleans up, even after a crash or Ctrl-C.

`axx up` keeps the system running between runs, so each edit-and-run cycle takes seconds.

It is fast. The [example suite](https://github.com/nimbusxr/axx/tree/main/examples/parcels) has 32 scenarios and 316 steps, covering REST, WireMock, PostgreSQL, MongoDB and Kafka. Against a running stack, it finishes in about a second.

## Axx's steps handle the hard parts

The steps of Axx's packs do the difficult work:
- OpenAPI validation
- database seeds and JSONB queries
- Avro with Schema Registry
- waiting for events to arrive
- checking calls to mocked services

All of them compose the same way. You build something (a request, an event, a selection), act on it, and assert on what came back. Any step can name the service it targets (`on parcels-db`). Ordinals such as `the 2nd selection` refer back to earlier ones. Once you know the pattern, a pack you have never used reads the same.

Step text is public API, and it never changes, so a feature file keeps working across every release.

## Extend it your way

Steps come in **packs**. A project uses only the packs it needs (`axx pack add rest sql`).

When Axx doesn't cover something, write a pack of your own in Go. Your steps work on the same scenario context as Axx's:
- the services the scenario registered
- its requests and responses
- its database selections
- its events

Use custom packs to reach a system Axx doesn't cover, or to write scenarios in your team's own words. `axx pack new ./steps` creates a pack. The first run builds it into Axx and caches the result. After that, your steps appear in `axx steps search`, `axx validate` and the agent skills like any other step. See [Write custom steps](/guides/write-custom-steps/).

## Built for coding agents

Axx is designed so coding agents never have to guess. They search the real step text instead of inventing it, check every line without starting anything, get failures as data with the expected and actual values, and read docs generated from the binary they are running. [Axx for agents](/explanations/axx-for-agents/) explains how.

## Test data that doesn't collide

Parallel scenarios only stay reliable if each one uses its own data. `axx lint` finds colliding ids and keys before they cause flaky failures. `axx fixtures` generates test data checked against your schemas and reports when it drifts. See [Isolate test data](/guides/isolate-test-data/) and [Fixture factories](/guides/fixture-factories/).

## Where to go next

| If you want to | Go to |
|---|---|
| Get a passing scenario in about ten minutes | [Quickstart](/tutorials/quickstart/) |
| Get a task done | [Guides](/guides/install/) |
| Look up a step, command or setting | [References](/references/steps/) |
| Understand the design | [How Axx works](/explanations/how-axx-works/) |

Axx is pre-release (`v0.x`): interfaces may change before `v1.0.0`.
