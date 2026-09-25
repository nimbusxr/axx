---
title: Your first suite
description: Build an acceptance suite for a realistic service step by step - its API and OpenAPI contract, a dependency it calls, and its database.
sidebar:
  order: 2
---

In this tutorial, we'll write an acceptance suite for Parcels, an example service that comes with Axx. Parcels is a parcel-delivery service with a PostgreSQL database. Shops ask it for price quotes and register their parcels with it, and it checks every address with an address service, which the example replaces with a mock.

Step by step, our suite will:

- call the service's API and check its answers;
- check every request and response against the API's OpenAPI document;
- check the call the service makes to the address service;
- check what the service writes to its database.

You'll need Docker with Compose v2, Git, and Axx (see the [Quickstart](/tutorials/quickstart/)). Plan on about 30 minutes, including a few minutes for Docker to build images the first time.

## Get the example

```sh
git clone https://github.com/nimbusxr/axx.git
cd axx/examples/parcels
```

The `infra` directory holds a Docker Compose file that runs Parcels with everything it needs.

## Create the suite

Make a directory for our suite:

```sh
mkdir my-suite
cd my-suite
```

Create `axx.yaml` in it:

```yaml title="axx.yaml"
version: 1

apps:
  parcels:
    dir: ../infra
    command: docker compose up --build
    ready:
      http:
        url: http://localhost:8400/health
      timeout: 10m
    cleanup: docker compose down -v --remove-orphans
```

This tells Axx to start Parcels with Docker Compose, to wait until its health check answers, and to remove the containers when it stops.

Next, choose the packs of steps the suite uses. We'll send requests to Parcels with `rest`, check what it asked the address service's mock with `mock`, and look in its database with `sql`:

```sh
axx pack add rest mock sql
```

```console
created axx-packs.yaml
added rest, mock, sql
```

## Start Parcels

```sh
axx up
```

```console
axx: starting apps in the background (logs: .axx/logs)
up: parcels (stop with `axx down`)
```

The first time, Docker builds the images, so this takes a few minutes. Parcels now keeps running in the background, and every run in this tutorial takes a fraction of a second.

## Ask for a price

Create a `features` directory:

```sh
mkdir features
```

Then create `features/quotes.feature`:

```gherkin title="features/quotes.feature"
Feature: Quotes

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |

  Scenario: A shop asks for a price
    Given a POST request to /api/quotes
    And a request payload using an application/json empty content template
    And the request payload properties are:
      | weightGrams | 1200                                   |
      | recipient   | {"postcode": "10115", "country": "DE"} |
    When the request is executed
    Then the response status code is 200
    And the response payload property priceCents is '690'
```

The scenario builds a request body from an empty JSON object: a 1.2 kg parcel to a Berlin postcode. Run it:

```sh
axx run
```

```console
axx: preparing rest, mock, sql (once; cached for later runs)
axx: ready in 31s
axx: reusing apps started by `axx up`: parcels
Feature: Quotes

  Scenario: A shop asks for a price  # features/quotes.feature:7
    ✓ Given the parcels service with the following properties:  (background)
        | url | http://localhost:8400 |
    ✓ Given a POST request to /api/quotes
    ✓ And a request payload using an application/json empty content template
    ✓ And the request payload properties are:
        | weightGrams | 1200                                   |
        | recipient   | {"postcode": "10115", "country": "DE"} |
    ✓ When the request is executed  (213ms)
        log: POST http://localhost:8400/api/quotes -> 200 OK (213ms, 112 bytes)
        attachment: request body (application/json, 68 B)
          {"weightGrams":1200,"recipient":{"postcode":"10115","country":"DE"}}
        attachment: response body (application/json, 112 B)
          {"zone":"DE-1","serviceLevel":"STANDARD","weightGrams":1200,"priceCents":690,"currency":"EUR","deliveryDays":2}
    ✓ Then the response status code is 200
    ✓ And the response payload property priceCents is '690'

1 scenario (1 passed)
7 steps (7 passed)
Finished in 213ms
```

Notice the first lines. Axx prepared itself with the suite's packs, which it does only once. And it didn't start anything this time: it used the Parcels service that `axx up` started. The price is 6.90 EUR, in cents.

## Check the API contract

Parcels publishes an OpenAPI document that describes its API. Add it to the service in the `Background`:

```gherkin title="features/quotes.feature"
  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
```

Run `axx run` again. The scenario still passes. Axx now checks every request and response against that document, so the suite notices when Parcels breaks its contract. We'll see the check at work in a moment.

## Start from the contract's example

The OpenAPI document also gives an example request for each operation. Add a second scenario at the end of `features/quotes.feature`:

```gherkin title="features/quotes.feature"
  Scenario: Heavier parcels cost more
    Given a POST request to /api/quotes
    And a request payload using an application/json content example
    And the request payload property weightGrams is '4500'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | weightGrams | 4500 |
      | priceCents  | 690  |
```

Run `axx run`. Both scenarios pass. Look at the new scenario's output:

```console
  Scenario: Heavier parcels cost more  # features/quotes.feature:18
    ✓ Given the parcels service with the following properties:  (background)
        | url     | http://localhost:8400              |
        | openapi | http://localhost:8400/openapi.json |
    ✓ Given a POST request to /api/quotes
    ✓ And a request payload using an application/json content example
    ✓ And the request payload property weightGrams is '4500'
    ✓ When the request is executed
        log: POST http://localhost:8400/api/quotes -> 200 OK (3ms, 112 bytes)
        attachment: request body (application/json, 94 B)
          {"weightGrams":4500,"serviceLevel":"STANDARD","recipient":{"postcode":"10115","country":"DE"}}
        attachment: response body (application/json, 112 B)
          {"zone":"DE-1","serviceLevel":"STANDARD","weightGrams":4500,"priceCents":690,"currency":"EUR","deliveryDays":2}
    ✓ Then the response status code is 200
    ✓ And the response payload properties are:
        | weightGrams | 4500 |
        | priceCents  | 690  |
```

Notice the request body. We only wrote the weight. Axx started from the example request in the OpenAPI document, which also has a service level and a recipient, and changed the weight.

## Break the contract on purpose

In the new scenario, change the weight to a string:

```gherkin
    And the request payload property weightGrams is '"heavy"'
```

Run `axx run`:

```console
    ✗ When the request is executed
        log: POST http://localhost:8400/api/quotes -> 400 Bad Request (1ms, 125 bytes)
        attachment: request body (application/json, 97 B)
          {"weightGrams":"heavy","serviceLevel":"STANDARD","recipient":{"postcode":"10115","country":"DE"}}
        attachment: response body (application/problem+json, 125 B)
          {"detail":"weightGrams must be an integer","instance":"/api/quotes","status":400,"title":"Bad Request","type":"about:blank"}
        OpenAPI validation failed for POST http://localhost:8400/api/quotes (status 400):
          - validation.request.body.schema.type: $.weightGrams: got string, want integer (POST request body for '/api/quotes' failed to validate schema)
        To relax a check, set its key (or a parent key) to WARN, INFO or IGNORE with "Given the OpenAPI validation levels are:" or openapi.levels in axx.yaml.
    ↷ Then the response status code is 200
    ↷ And the response payload properties are:
        | weightGrams | 4500 |
        | priceCents  | 690  |
```

The double quotes make the value a string. The contract says `weightGrams` is an integer, and Axx fails the request step with the reason: `$.weightGrams: got string, want integer`. The steps after it are skipped.

Change the value back to `'4500'` and run `axx run` again. Both scenarios pass.

## Check what Parcels asked the address service

To price a parcel, Parcels asks the address service which delivery zone serves the recipient's postcode. In this example, the address service is a mock running on port 8081. Let's check that the call happened.

Register the mock in the `Background`, and add a last step to the first scenario. Your file now looks like this:

```gherkin title="features/quotes.feature"
Feature: Quotes

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked addresses service with the following properties:
      | url | http://localhost:8081 |

  Scenario: A shop asks for a price
    Given a POST request to /api/quotes
    And a request payload using an application/json empty content template
    And the request payload properties are:
      | weightGrams | 1200                                   |
      | recipient   | {"postcode": "10115", "country": "DE"} |
    When the request is executed
    Then the response status code is 200
    And the response payload property priceCents is '690'
    And the mocked GET request to /v1/postcodes/DE/10115 named zone-lookup was received by addresses

  Scenario: Heavier parcels cost more
    Given a POST request to /api/quotes
    And a request payload using an application/json content example
    And the request payload property weightGrams is '4500'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | weightGrams | 4500 |
      | priceCents  | 690  |
```

Run `axx run`. Both scenarios pass:

```console
    ✓ Then the response status code is 200
    ✓ And the response payload property priceCents is '690'
    ✓ And the mocked GET request to /v1/postcodes/DE/10115 named zone-lookup was received by addresses
```

Axx asked the mock which requests it had received, and found Parcels' `GET /v1/postcodes/DE/10115`.

## Check the database

Now we'll register a parcel through the API and check that Parcels stored it. Create `features/parcels.feature`:

```gherkin title="features/parcels.feature"
Feature: Parcels

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |

  Scenario: A registered parcel is stored
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference | PX-TUTORIAL-1 |
      | sender    | my-shop       |
    When the request is executed
    Then the response status code is 201
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-TUTORIAL-1 |
      | sender    | my-shop       |
    And the selection has 1 row
```

The `Background` now also registers Parcels' database. Run `axx run`, and look at the end of the output:

```console
    ✓ Then the response status code is 201
    ✓ And a selection of rows is retrieved from the parcels.parcels table where:
        | reference | PX-TUTORIAL-1 |
        | sender    | my-shop       |
    ✓ And the selection has 1 row

3 scenarios (3 passed)
26 steps (26 passed)
Finished in 22ms
```

The new scenario registered a parcel through the API. Then it selected the rows of the `parcels.parcels` table with that reference and sender, and found exactly one: Parcels stored what it was sent.

Run `axx run` once more. This time the new scenario fails:

```console
        log: POST http://localhost:8400/api/parcels -> 409 Conflict (1ms, 135 bytes)
          {"detail":"parcel PX-TUTORIAL-1 is already registered","instance":"/api/parcels","status":409,"title":"Conflict","type":"about:blank"}
    ✗ Then the response status code is 201
```

The parcel from the first run is still in the database, and references are unique. Acceptance tests run against the service's real data, so data a scenario creates stays behind. [Isolate test data](/guides/isolate-test-data/) shows how suites deal with that. For now, stopping Parcels clears everything.

## Stop Parcels

```sh
axx down
```

```console
stopped: parcels
```

Axx stopped Parcels and ran its cleanup, which removed the containers and their data. The next `axx up` starts from empty databases.

## What you've done

You wrote a suite for a service with a database and a dependency, and checked four things from the outside:

- the answers of its API;
- its requests and responses against its OpenAPI contract;
- the call it makes to another service;
- what it writes to its database.

You didn't write any test code: every line is a step Axx provides. The complete suite for Parcels, with events, a document store and more, is in `examples/parcels/acceptance`.

Next, in [Test with an agent](/tutorials/with-an-agent/), we'll hand an acceptance criterion to a coding agent and review the scenario it writes.
