---
name: axx-acceptance-tests
description: Write and run acceptance tests with axx, black-box against services (REST and OpenAPI, WireMock, SQL, MongoDB, Kafka, logs, files, web apps in real browsers, cloud services). Use when turning acceptance criteria into .feature files, adding scenarios, or editing axx.yaml.
license: Apache-2.0
---

# Writing acceptance tests with axx

The AGENTS.md section has the workflow and the rules. This adds the shape of a feature and what the steps' tables take.

## A feature

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

- Register the services in the `Background`. The first one registered is the default; name another with `... on <service>`.
- With an `openapi` URL, a request or response that breaks the contract fails the `executed` step. A scenario that sends an invalid request on purpose relaxes that rule first: `Given the OpenAPI validation levels are:` with `| validation.request.body | IGNORE |`.
- A scenario that checks the outcome with another request adds both in the Givens and executes both in the When:

```gherkin
    Given a POST request to /api/parcels
    ...
    And a 2nd ordered GET request to /api/parcels/PX-REG-0002
    When the request is executed
    And the 2nd ordered request is executed
    Then the response status code is 400
    And the 2nd ordered response status code is 404
```

## Tables

- **REST payloads.** A row sets one property: `recipient.name` sets it inside `recipient`, creating the object when the payload lacks it. `"53111"` in double quotes is a string, `53111` a number, `null` JSON null, `undefined` removes the property. Single quotes are part of a table value. A first row naming the columns is optional.
- **Other packs' tables** say what their rows hold in `axx steps show <id>`: the SQL JSON checks compare values as text, for example, where REST compares JSON values.

## Waiting

A waiting step (`within 10s a selection of at least 1 row ...`) checks again until it holds, and fails when it does not in time. Never add sleeps.

## Lay out the suite by feature

Each feature gets a folder named after it, holding every file only it uses (seeds, payloads, messages, expected files, fixture factories). A step finds a file next to its feature file first, then in `resources`, then beside `axx.yaml`: write `a seeds/shop-lark.yaml db seed`, not the path from the project root. A shared folder is for what a second feature needs; mocks' mappings and compose files stay with the services.

## More

- `references/parameter-types.md`: what `{ordinal}`, `{word}`, `{duration}` and the others match.
- `references/config.md`: a commented `axx.yaml`.
- Scenarios that must run alone (database triggers, say): tag them with a tag `run.exclusive` lists, usually `@isolated`.
