---
title: Reports
description: Choose console output, write JUnit, HTML, Cucumber JSON, Cucumber Messages and agent reports, and get machine-readable results with --json.
---

## Console output

| Format | What you see | When |
| --- | --- | --- |
| `pretty` | every scenario and step, tables, logs, failures, a summary | the default in a terminal |
| `progress` | one character per scenario, then failures and a summary | long suites |
| `compact` | failures only, and one summary line | automatic for coding agents (`--compact`) |

```console
$ axx run --format progress features/register-parcels.feature
...F....

Feature: Register parcels

  Scenario: A reference can only be registered once  # features/register-parcels.feature:67
    ...

Failed scenarios:
  x A reference can only be registered once  # features/register-parcels.feature:67
      rerun: axx run features/register-parcels.feature:67

8 scenarios (1 failed, 7 passed)
96 steps (1 failed, 1 skipped, 94 passed)
Finished in 0.6s
```

Problems that belong to no single scenario are listed under `Run failures:` after the failed scenarios, and fail the run. The one Axx reports today is a call to a [mocked dependency](/guides/mock-dependencies/#check-the-dependencys-contract) that broke its contract when no scenario checked the call. JUnit reports them as an `axx run` test suite, the agent report as `runErrors`, and Cucumber Messages in `TestRunFinished`.

`--compact` is turned on automatically when an agent is detected (`CLAUDECODE`, `CODEX_SANDBOX`, `GEMINI_CLI`, `CURSOR_AGENT`, `AGENT` or `AI_AGENT` is set) and stdout is not a terminal. `--no-color` or `NO_COLOR` turns colors off.

## Report files

Add `--format NAME:FILE` once per report:

```sh
axx run \
  --format pretty \
  --format junit:build/axx/junit.xml \
  --format html:build/axx/report.html
```

| Name | Output | Use it for |
| --- | --- | --- |
| `junit` | JUnit XML, one test case per scenario | CI test tabs (GitHub, GitLab, Jenkins) |
| `html` | a single self-contained HTML report | humans, as a CI artifact |
| `cucumber-json` | the classic Cucumber JSON format | tools that read Cucumber reports |
| `messages` | [Cucumber Messages](https://github.com/cucumber/messages) as NDJSON | the Cucumber ecosystem, custom tooling |
| `agent` | the compact JSON report below | agents, scripts |
| `teamcity` | TeamCity service messages: a live tree of features, scenarios and steps | IDE test runners (the axx IntelliJ plugin and VS Code extension run it) |

The same list can be the default in `axx.yaml`:

```yaml title="axx.yaml"
run:
  reporters:
    - pretty
    - junit: build/axx/junit.xml
    - html: build/axx/report.html
```

An unknown name fails with [`AXX-E0600`](/references/error-codes/#axx-e0600).

## Machine-readable results

`axx run --json` prints one [JSON envelope](/references/json-output/) whose `data` is the run report. `--format agent:FILE` writes the same report to a file while the console shows another format. Only failures are listed, so the report stays small however large the suite is. [JSON output](/references/json-output/#the-run-report) describes every field.

The exit code says what kind of result it was without parsing any output; see [exit codes](/references/error-codes/#exit-codes).
