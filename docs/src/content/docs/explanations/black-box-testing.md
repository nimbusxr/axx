---
title: Black-box acceptance testing
description: Why Axx tests services from the outside, through the same interfaces their clients use, and what that buys you over in-process tests.
---

An **acceptance test** checks that the system does what was agreed: the acceptance criteria of a story, written so the people who agreed on them can read the test. A **black-box** test checks it the way a client would, through the system's public interfaces, with no knowledge of its insides.

Axx does both. A scenario sends real requests to a running service, puts real rows in its real database, publishes real events to its broker, and checks what comes back and what the service did to the world.

## Why from the outside

- **The test survives refactoring.** Rename a class, swap a framework, rewrite the service in another language: the scenarios still describe the same behavior and still run.
- **It tests what ships.** The artifact under test is the one you deploy (a container, a binary), started the way you start it, configured the way you configure it. Serialization, framework wiring, database constraints and migrations are all in the loop.
- **It needs no access to the code.** The service does not depend on Axx or on any test library. A team can write acceptance tests for a service in any language, and an agent can write them without reading the implementation.
- **It reads like the requirement.** A scenario is the acceptance criterion in structured English. When it fails, the name says which promise broke.

## What it costs, and how Axx pays it

Black-box suites have a reputation for being slow and flaky. The causes are specific, and so are the fixes:

| Cause | What Axx does |
| --- | --- |
| Starting the system for every test class | starts apps once per run, or once per session with `axx up` |
| Tests run one at a time | runs scenarios in parallel by default |
| Tests share and corrupt data | makes data isolation checkable with `axx lint` and fixture identities |
| Waiting with fixed sleeps | readiness checks for apps, polling steps for asynchronous results |
| Brittle hand-written payloads | payloads from OpenAPI examples and fixture factories |
| Opaque failures | expected and actual values, the last request and response, a rerun command |

## Where it fits

Black-box acceptance tests sit on top of unit and integration tests; they do not replace them. Use them for the behavior a client or another team relies on: the API contract, the events you publish, the data you persist, the calls you make to dependencies. Keep edge cases of pure logic in unit tests, where they are cheaper.

## Contracts in both directions

A service has two contracts: the API it provides and the APIs it consumes. Axx checks both from the outside:

- **Provided**: every request and response is validated against your OpenAPI document ([OpenAPI as contract](/explanations/openapi-contract/)).
- **Consumed**: dependencies are replaced by WireMock mocks, which can themselves be validated against the provider's OpenAPI document ([Mock dependencies](/guides/mock-dependencies/)).
