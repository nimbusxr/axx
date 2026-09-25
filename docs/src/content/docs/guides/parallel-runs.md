---
title: Run in parallel
description: Control how many scenarios run at once, keep scenarios that cannot share infrastructure on their own with run.exclusive, and randomize order to flush out hidden dependencies.
---

Axx runs scenarios in parallel by default, one per CPU. Parallelism is what keeps a suite of hundreds of black-box scenarios fast, and it only works if scenarios do not step on each other.

## Workers

```yaml title="axx.yaml"
run:
  workers: auto     # default: the number of CPUs; or a number
```

```sh
axx run --workers 4
axx run -w 1        # one at a time, for debugging an ordering problem
```

Each scenario runs start to finish on one worker. Steps inside a scenario always run in order.

## Scenarios that must run alone

Some scenarios change shared state in a way no other scenario can tolerate: they install database triggers, change a feature flag for the whole service, or restart a dependency. Tag them and list the tag in `run.exclusive`:

```yaml title="axx.yaml"
run:
  exclusive: ["@isolated"]
```

```gherkin
@isolated
Feature: Database failures

  Background:
    Given a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |

  Scenario: A brief database failure does not fail the registration
    Given a before insert trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
      | reference | PX-DBF-3001 |
```

Axx runs every other scenario in parallel first, then the exclusive ones one at a time. Tags are inherited, so tagging the feature covers every scenario in it.

## Order

```sh
axx run --order random          # a new random order each run
axx run --order random:4242     # repeat a specific order
```

`run.order` in `axx.yaml` sets the default (`defined` unless you change it). A suite that passes in `defined` order but fails in a random one has scenarios that depend on each other.

## Timeouts

```yaml title="axx.yaml"
run:
  timeouts:
    step: 60s        # default 10m
    scenario: 5m     # default: none
    hook: 2m         # default 2m
```

Durations are strings such as `90s` or `5m`, or a number of seconds. A step that runs over is cancelled and reported with `error.kind: timeout`. For behavior that takes time, use a polling step (`within 10s ...`) instead of a longer timeout.

## When parallel runs fail

A scenario that passes alone (`axx run features/x.feature:12`) but fails in a full run almost always shares data with another scenario. In order of preference:

1. Give it unique data, and enforce that with [`axx lint`](/guides/isolate-test-data/).
2. Assert on its own data only (filter selections by its ids, name mock request patterns by its URLs).
3. If it truly cannot share, tag it for `run.exclusive`.

Lowering `--workers` hides the problem instead of fixing it.
