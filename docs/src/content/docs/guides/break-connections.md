---
title: Break connections
description: Check what your services do when a connection to a database, a cache, a broker or an API is cut off, slow, hanging, reset or closed halfway through an answer, through real Toxiproxy.
---

A dependency rarely fails by answering with an error. Its connection goes away, or hangs, or a query that took 5 ms takes 5 seconds. The `faults` pack puts your service through that, with [Toxiproxy](https://github.com/Shopify/toxiproxy) between it and what it depends on, and you check what your service does.

```gherkin
Scenario: A brief outage does not fail the registration
  Given the parcels-db connection is cut off for 1s
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload property reference is 'PX-FLT-9703'
  When the request is executed
  Then the response status code is 201
  And the parcels log has an entry matching 'msg="the database did not answer, retrying" reference=PX-FLT-9703'
```

Add the pack to the project with `axx pack add faults` ([Choose packs](/guides/use-packs/)).

## Put Toxiproxy between them

Run Toxiproxy with the rest of your test environment, and point your service at it. Declare the connections in its config, as your service needs their ports before any scenario runs, as the parcels example does:

```yaml title="compose.yaml"
services:
  toxiproxy:
    image: ghcr.io/shopify/toxiproxy:2.13.0
    command: ["-host=0.0.0.0", "-config=/etc/toxiproxy/toxiproxy.json"]
    ports:
      - '8474:8474'
    volumes:
      - './toxiproxy/toxiproxy.json:/etc/toxiproxy/toxiproxy.json:ro'

  app:
    environment:
      PARCELS_DB_URL: postgres://parcels:parcels@toxiproxy:15432/parcels?sslmode=disable
```

```json title="toxiproxy/toxiproxy.json"
[
  {"name": "parcels-db", "listen": "0.0.0.0:15432", "upstream": "postgres:5432", "enabled": true}
]
```

Then register it once, usually in the `Background`. A connection in a step is the name of a proxy in that config:

```gherkin
Given the parcels fault proxy with the following properties:
  | url | http://localhost:8474 |
```

Axx's own connections, for a seed or a check, go straight to the database, so they keep working while your service's connection fails.

## What a connection can do

| Step: `the parcels-db connection ...` | What the service sees |
| --- | --- |
| `is cut off` | Connections close, and new ones are refused |
| `is slowed by 2s` | Everything takes that much longer |
| `is limited to 100 KB/s` | A large answer takes its time |
| `hangs` | No answer, until its own timeout gives up |
| `hangs for 5s, then closes` | No answer, then the connection closes |
| `is reset`, `is reset after 1s` | The connection is reset |
| `takes 2s to close` | Closing the connection is slow; what it carries is not |
| `breaks into 100-byte pieces, 1ms apart` | An answer arrives a little at a time |
| `closes after 50 KB` | An answer is cut off partway |
| `loses 10% of its data` | No error: what it waits for just does not come |

Every one of them hits the connections open now, which is what a service's connection pool holds, and ends when the scenario does. Only what that scenario did is undone. `When the parcels-db connection is restored` ends them sooner.

All of them but `is cut off` can say more, in this order. `is cut off` takes only the last:

```gherkin
Given the parcels-db connection hangs going out on 30% of connections for 5s
```

- **`coming back`** (the default) **or `going out`** is the direction it applies to. With `coming back`, what the service sends still arrives and only the answer is lost, so a write can be made though the service never hears so. With `going out`, nothing the service sends arrives. A driver that checks a connection before it uses one, as a pool's often does, waits on that check first, so the write may not be sent at all.
- **`on 30% of connections`** gives each new connection the fault or not, by chance.
- **`for 5s`** ends it by itself. A request step waits for its answer, so no restore step can run during it: this is how to test a service retrying through an outage.

Toxiproxy's other toxics, your own included, take their type and attributes:

```gherkin
Given the parcels-db connection has a latency toxic with the following attributes:
  | latency  | 100        |
  | jitter   | 50         |
  | stream   | "upstream" |
  | toxicity | 0.5        |
```

## Prove what did not use a connection

A connection cut off proves that an answer came from somewhere else:

```gherkin
Scenario: A cached tracking view answers while the database is cut off
  Given a redis/tracking-PX-FLT-9701.yaml redis seed
  And the parcels-db connection is cut off
  And a GET request to /api/parcels/PX-FLT-9701/tracking
  When the request is executed
  Then the response status code is 200
```

## Pick the right level

Toxiproxy sees bytes, not requests, so it cannot pick a URL, a query or a row. To fail one of those, use the step made for it:

| To fail | Use |
| --- | --- |
| One HTTP request | A [WireMock mapping](/guides/mock-dependencies/) |
| One row's insert | A [database trigger](/guides/seed-and-query-sql/) |
| Everything on a connection, whatever its protocol | This pack |

## Scenarios that share a connection

A fault applies to everyone using the connection, so scenarios running at the same time feel each other's. To run the scenarios that use this pack alone, after the parallel ones, list it in `run.exclusiveUses` ([Run in parallel](/guides/parallel-runs/)):

```yaml title="axx.yaml"
run:
  exclusiveUses: [faults]
```
