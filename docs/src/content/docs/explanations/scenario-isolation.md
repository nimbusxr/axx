---
title: Scenario isolation and test data
description: Why Axx shares infrastructure between scenarios but never data, how that makes parallel runs safe, and the tools that enforce it.
---

A fast black-box suite shares infrastructure: one database, one broker, one WireMock, one running service for hundreds of scenarios running in parallel. It stays correct only if scenarios never share **data**. Axx is built around that rule.

## What is isolated for you

Each scenario has its own **world**: the services it registered, the requests it built and the responses it received, its selections and its events. Nothing in the world leaks into another scenario, and a scenario always runs on a single worker.

## What you isolate

Everything outside Axx persists: rows in the database, documents in MongoDB, events on topics, requests in the WireMock journal. Those are visible to every scenario, including ones running at the same moment and ones in the next run. So:

1. **Every scenario owns its data.** References, ids, keys and emails belong to one scenario: `PX-REG-1004`, not `test`. A scenario that registers a parcel for the sender `shop-example` and then counts that sender's parcels will pass alone and fail beside any other scenario that does the same.
2. **Assert on your own data only.** Select rows by your ids; name mock request patterns by URLs that contain your ids; consume events by your keys.
3. **Do not rely on cleanup.** Data from earlier runs, failed and interrupted ones included, is still there. A scenario must pass beside everyone else's data. A scenario that inserts a fixed id (a seed, a registration with a fixed reference) needs that id to be free, so it runs again only on a fresh environment: `axx run` starts from empty databases, and against `axx up`, restart with `axx down` and `axx up`.

## Enforcing it

Conventions drift, so Axx makes them checkable:

- **`axx lint`** extracts ids from seeds, payloads and features and fails when a value appears where it must be unique ([Isolate test data](/guides/isolate-test-data/)).
- **Fixture identities**: an `identity:` in a factory spec derives unique values for every fixture and generates the matching lint rule ([Fixture factories](/guides/fixture-factories/)).
- **Randomized order**: `axx run --order random` exposes scenarios that only pass after another one ran.

## When sharing is unavoidable

Some scenarios change the environment for everyone: a database trigger that makes inserts fail, a global feature flag, a dependency that is stopped on purpose. Tag them with a tag listed in `run.exclusive` (for example `@isolated`). Axx runs them one at a time after the parallel phase, so they never overlap with another scenario ([Run in parallel](/guides/parallel-runs/)).

## Why not reset the database between scenarios

Truncating tables or restoring snapshots makes scenarios serial (a reset in one breaks another that is running), slow, and blind to problems that only appear with realistic data volumes. Unique data costs a naming convention and gives you parallel runs against a long-lived environment, including `axx up` sessions that last all day.
