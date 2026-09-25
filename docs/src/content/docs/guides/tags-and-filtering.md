---
title: Tags and filtering
description: Select which scenarios run by path, line, tag expression or name, and set defaults in axx.yaml.
---

Every way of selecting scenarios combines: paths narrow the files, then tags and names filter the scenarios in them.

## By path and line

```sh
axx run                                   # run.paths from axx.yaml (default: features)
axx run features/quotes.feature           # one file
axx run features/rest                     # a directory
axx run features/quotes.feature:14        # the scenario on line 14
axx run features/a.feature:14 features/b.feature:9   # several
```

A line number selects the scenario that starts on that line or contains it, so the line of any step works. That is why the `rerun` command in a failure report is always a `file:line`.

## By tag

Tag features, rules, scenarios and examples:

```gherkin
@parcels
Feature: Register parcels

  @smoke
  Scenario: A shop lists its own parcels
    Given a GET request to /api/parcels?sender=lark-ceramics
    When the request is executed
    Then the response status code is 200

  @wip
  Scenario: A cancelled parcel is gone
    Given a DELETE request to /api/parcels/PX-TAG-0001
    When the request is executed
    Then the response status code is 204
```

Select with a [tag expression](https://cucumber.io/docs/cucumber/api/#tag-expressions):

```sh
axx run --tags @smoke
axx run --tags "@parcels and not @wip"
axx run -t "(@smoke or @critical) and not @slow"
```

Tags are inherited: a scenario has its own tags plus those of its feature, rule and examples.

## By name

```sh
axx run --name "registered once"           # a regular expression on the scenario name
axx run -n "^A shop" -n "cancelled"         # repeatable: matches either
```

## Defaults in axx.yaml

```yaml title="axx.yaml"
run:
  paths: [features]
  tags: "not @wip and not @ignore"
```

`--tags` on the command line replaces `run.tags`.

## Tags that change behavior

- Tags listed in `run.exclusive` make scenarios run alone, after the parallel phase ([Run in parallel](/guides/parallel-runs/)).
- With `active.enabled`, tags decide which apps start ([Manage the app lifecycle](/guides/manage-app-lifecycle/#start-only-what-a-run-needs)).

## Rerun what failed

```sh
axx run --rerun-file build/axx/rerun.txt   # writes file:line of each failed scenario
axx run $(cat build/axx/rerun.txt)         # run only those
```

## Preview a selection

`--dry-run` matches every selected step without starting apps or executing anything, which shows what a filter selects:

```sh
axx run --tags @smoke --dry-run
```
