---
title: Why Axx
description: Axx verifies running software against executable acceptance criteria, black-box, so implementations can change without losing the definition of correct. It's the behavioral verification layer for agentic software factories.
---

Software is increasingly produced by coding agents, but the responsibility for correctness hasn't gone away.

As implementations become easier to generate and replace, engineering teams need durable, independently executable definitions of expected behavior. A test suite that imports the code it checks changes with that code; a definition of correct shouldn't.

Axx approaches this through black-box acceptance testing. It verifies running systems through their observable interfaces (their HTTP APIs and web apps, their databases, their event streams and the dependencies they call), so implementations can evolve without the acceptance criteria being tied to their internal structure.

The result is a verification framework for conventional engineering workflows and for the emerging generation of agentic software factories, where agents build and change the implementation and Axx checks that it still does what it must.

Three ideas run through it:
- **Intent.** Acceptance criteria state what the system must do, as scenarios people and agents can review. They're written in Gherkin, and they stay meaningful while the implementation changes.
- **Freedom of implementation.** Refactor a service, switch frameworks or rewrite it in another language: while the external contract holds, the same scenarios apply.
- **Independent verification.** An agent can use Axx to check its own work, but the code it wrote isn't what defines success. The scenarios are, and your team decides who approves them.

When a scenario passes, the behavior it describes works in the running system, not in a mock of it or in one unit on its own. Dependencies outside the system can be mocked (Axx checks the calls made to them), while the system itself runs for real.

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
- checks that the service asked its downstream address service (mocked) about the postcode;
- checks what the service stored in PostgreSQL, down to a JSON column.

Every line is a step from one of Axx's packs, so there is no step code to write.

## Test the service, not the code

Axx never loads your code. The service can be written in any language, and the tests keep passing through refactors and rewrites because they only depend on what the service does. The people who wrote the acceptance criteria can read them too. See [Black-box testing](/explanations/black-box-testing/).

Axx's packs cover:

| Area | What you get |
|---|---|
| REST | Requests and responses validated against your OpenAPI document |
| Mocked dependencies | Calls to them verified through WireMock |
| SQL | PostgreSQL, MySQL, SQLite and SQL Server: seeds, selections, JSON columns, locks and triggers |
| MongoDB | Seeding and querying |
| Kafka | Publishing and consuming events, with Avro and Schema Registry |
| Logs | The entries your services log, in files or sent over UDP, TCP or HTTP |
| Files | The files your services write: their content, their text (PDF, Word, Excel...) and their rows |
| Web apps | Real browsers through Playwright, with screenshots, accessibility audits, network control, Lighthouse scores and JavaScript coverage |
| Clouds | S3, SQS, SNS, EventBridge and DynamoDB; Cloud Storage, Pub/Sub, BigQuery and Firestore; Blob Storage and Service Bus |

## One binary with nothing to wire up

Axx needs no JVM, no build plugin and no test-runner glue. `axx run` does the whole job:
1. It starts the services listed in `axx.yaml` and waits until they are healthy.
2. It runs the scenarios in parallel.
3. It stops everything and cleans up, even after a crash or Ctrl-C.

`axx up` keeps the system running when you want to inspect it.

It is fast. The [example suite](https://github.com/nimbusxr/axx/tree/main/examples/parcels) has 267 scenarios and 2,814 steps, covering REST, WireMock, PostgreSQL, MongoDB, Kafka, logs, files and a web app in real browsers. They run in parallel, and what takes the time is the services, not Axx.

## Axx's steps handle the hard parts

The steps of Axx's packs do the difficult work:
- OpenAPI validation
- database seeds and JSONB queries
- Avro with Schema Registry
- waiting for events to arrive
- checking calls to mocked services

All of them compose the same way. You build something (a request, an event, a selection), act on it, and assert on what came back. A step can name the service it targets (`on parcels-db`) when a scenario uses more than one. Ordinals such as `the 2nd selection` refer back to earlier ones. Once you know the pattern, a pack you have never used reads the same.

Step text is public API, and it never changes, so a feature file keeps working across every release.

## Extend it your way

Steps come in **packs**. A project uses only the packs it needs (`axx pack add rest sql`).

When Axx doesn't cover something, write a pack of your own in Go. Your steps work on the same scenario context as Axx's:
- the services the scenario registered
- its requests and responses
- its database selections
- its events

Use custom packs to reach a system Axx doesn't cover, or to write scenarios in your team's own words. `axx pack new ./steps` creates a pack. The first run builds it into Axx and caches the result. After that, your steps appear in `axx steps`, `axx validate` and the MCP server's tools like any other step. See [Write custom steps](/guides/write-custom-steps/).

## Built for coding agents

Axx is designed so coding agents never have to guess. They search the real step text instead of inventing it, check every line without starting anything, get failures as data with the expected and actual values, and read docs generated from the binary they are running. [Axx for agents](/explanations/axx-for-agents/) explains how.

## Test data that doesn't collide

Parallel scenarios only stay reliable if each one uses its own data. `axx lint` finds colliding ids and keys before they cause flaky failures. `axx fixtures` generates test data checked against your schemas and reports when it drifts. See [Isolate test data](/guides/isolate-test-data/) and [Fixture factories](/guides/fixture-factories/).

## Where to go next

| If you want to | Go to |
|---|---|
| Get a passing scenario in about ten minutes | [Quickstart](/tutorials/quickstart/) |
| Get a task done | [Guides](/guides/install/) |
| Look up a pack, a step, a command or a setting | [References](/references/packs/) |
| Understand the design | [How Axx works](/explanations/how-axx-works/) |

Axx is pre-release (`v0.x`): interfaces may change before `v1.0.0`.
