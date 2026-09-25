---
title: OpenAPI as the contract
description: Why Axx treats your OpenAPI document as the single source of truth for an API, validates every exchange against it, and builds payloads from its examples.
---

An API has one contract, and it should live in one place. In Axx that place is the OpenAPI document. Scenarios do not restate the contract; they rely on it.

## The document is the source of truth

Some contract-testing approaches make the tests the source of truth and generate stubs or documents from them. Then the published OpenAPI document and the tested behavior can disagree, and nobody notices until a client breaks. Axx goes the other way: the document the service publishes is the contract, and the acceptance suite proves the service honors it.

When a REST service is registered with an `openapi` property, every request a scenario sends and every response it receives is validated against that document. A scenario that checks only the status code still catches a missing required field, a wrong type or an undocumented status, because the validator checks everything the scenario did not.

## Examples are test data

Documents already carry examples for their operations. Axx uses them as the starting payload (`a request payload using an application/json content example`), so a scenario lists only the values it is about. Two good effects follow:

- The examples in your published documentation are exercised on every run, so they stay correct.
- When a schema gains a field, you update the example once instead of every scenario.

## Strict by default, relaxed on purpose

Every validation rule starts at `ERROR`. A negative test, which sends an invalid request on purpose to check that the service rejects it, relaxes exactly the rule it breaks, in that scenario only:

```gherkin
Given the OpenAPI validation levels are:
  | validation.request.body.schema.maximum | IGNORE |
```

That line comes from a scenario that sends a 31 kg parcel to check that the service refuses parcels over 30 kg: the request breaks the document's `maximum`, and nothing else is relaxed.

The relaxation is visible in the feature file, next to the behavior that needs it. Suite-wide relaxations in `axx.yaml` exist for documents you do not control; using them for your own contract defeats the point.

## Both sides of a dependency

A contract has two sides, and Axx checks both. Your service's own document is checked by the REST steps: the provider side, where your service keeps its promise to its clients. The documents of the services your service calls are checked by the WireMock image that mocks them: the consumer side, where your service calls its dependencies correctly and the mocks answer as the real services would, so a stub cannot promise something the real API never does.

The two sides have separate settings. Relaxing a rule of your own contract for a negative test says nothing about a dependency's contract, and a stub that answers off-contract on purpose says nothing about yours. The provider validates its implementation against its document; the consumer validates its calls and mocks against the same document. Neither side needs the other's tests.

See [Validate against OpenAPI](/guides/validate-openapi/) for the steps and settings, and [Mock dependencies](/guides/mock-dependencies/) for the consumer side.
