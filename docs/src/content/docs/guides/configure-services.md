---
title: Configure services
description: Register the REST APIs, mocks, databases and brokers a scenario talks to, name them, and keep URLs and credentials out of feature files.
---

A **service** is something a scenario talks to: your REST API, a WireMock server, a SQL or MongoDB database, a Kafka cluster. Scenarios register services by name, usually in a `Background`, and later steps use them.

## Register services in a Background

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
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://localhost:27017/parcels?authSource=admin |
      | user     | parcels                                            |
      | password | parcels                                            |
    And the events kafka service with the following properties:
      | brokers | localhost:9092 |
```

Each kind of service has its own registration step and properties, listed on its pack's page: [REST](/references/steps/rest/) (an `openapi` property turns on [validation](/guides/validate-openapi/)), [Mocks](/references/steps/mock/), [SQL](/references/steps/sql/), [MongoDB](/references/steps/mongo/) and [Kafka](/references/steps/kafka/).

## The default service and named services

The first service of a type registered in a scenario is the default. Steps without a service name use it:

```gherkin
Given a GET request to /api/parcels/PX-REG-1001
When the request is executed
Then the response status code is 200
```

When a scenario uses two services of the same type, name the one you mean with the `on <service>` form of the step:

```gherkin
Given a GET request to /api/parcels/PX-REG-1001 on parcels
When the request is executed on parcels
Then the response status code is 200 on parcels
```

Most steps that address a service have both forms (a few mock steps always name it); see [How steps read](/references/steps/#services).

## Keep values out of feature files

Values in service tables and in `axx.yaml` are interpolated ([syntax](/references/config/#interpolation)). Put what differs between machines, and every secret, in properties and environment variables:

```yaml title="axx.yaml"
properties:
  local.host: localhost
  db.password: ${env:PARCELS_DB_PASSWORD:-parcels}
```

```gherkin
Given a parcels-db database with the following properties:
  | url      | postgres://${sys:local.host}:5432/parcels |
  | user     | parcels                                   |
  | password | ${sys:db.password}                        |
```

Override a property for one run with `axx run -D local.host=docker`.

## Differences between environments

Use a **profile** for settings that differ in CI or on another machine:

```yaml title="axx.yaml"
profiles:
  ci:
    properties:
      local.host: docker
```

Select it with `axx run --profile ci` or `AXX_PROFILE=ci`. A profile can also live in its own file, and personal overrides in an `axx.local.yaml`; [Finding and merging files](/references/config/#finding-and-merging-files) has the order.

## Resource paths

Steps that take a file (`seeds/manifest-kestrel.yaml`, `kafka/scan-delivered.json`, `schemas/depot-scan.avsc`) resolve it against the `resources` directories in order, then the directory of `axx.yaml`:

```yaml title="axx.yaml"
resources: [acceptance, ../shared-fixtures]
```

If a file is not found, the step fails with [`AXX-E0301`](/references/error-codes/#axx-e0301) and lists the directories it searched.
