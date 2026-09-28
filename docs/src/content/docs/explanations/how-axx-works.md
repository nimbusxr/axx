---
title: How Axx works
description: What happens between typing axx run and reading the result - configuration, step matching, app lifecycle, parallel execution, packs, the world and reporting.
---

Axx is one Go binary with its own Cucumber executor. It reads Gherkin, matches every step to a definition, starts the system under test, runs the scenarios against it from the outside, and reports.

```text
axx.yaml ─┐
features ─┼─▶ load & validate ─▶ match steps ─▶ start apps ─▶ run scenarios ─▶ report ───────────────▶ stop apps
packs    ─┘                     (registry)      (lifecycle)   (workers)        (pretty, junit, html…)  (cleanup)
```

## 1. Load

Axx finds `axx.yaml` by walking up from the working directory, applies the selected profile and `axx.local.yaml`, expands `${env:...}` and `${sys:...}`, and validates the result against the [schema](/references/config/). It then parses the feature files with the official Cucumber Gherkin parser and applies the path, line, tag and name filters.

## 2. Match

Every step line is matched against the **registry**: the steps of the packs the project loads. Steps are [Cucumber Expressions](https://github.com/cucumber/cucumber-expressions) with custom parameter types such as `{ordinal}` and `{service}`. Optional segments like `[[ on {service}]]` register every variant of a step.

A line that matches nothing is *undefined*; a line that matches two definitions is *ambiguous*. `axx validate`, `axx explain` and `axx run --dry-run` are this phase alone: they find both without starting an app or running a step. In a run, a scenario stops at an undefined or ambiguous step, and the report suggests the closest real steps or lists the candidates.

## 3. Start the apps

The lifecycle manager starts `apps` one after another in the order `axx.yaml` declares them, or, when apps declare `dependsOn`, in dependency order with independent apps in parallel. Each runs in its own process group. It polls every `ready` check until they pass or time out. With `active.enabled`, only the apps the selected scenarios' tags call for start, with the apps they depend on. With `axx up`, a background supervisor keeps them running and later runs skip this step.

## 4. Run

Scenarios run in parallel on `run.workers` workers; steps within a scenario run in order. Scenarios tagged with a `run.exclusive` tag run one at a time after the parallel phase.

Each scenario gets a fresh **world**: its registered services and the state each pack keeps (requests built and responses received, selections, events, browser pages). Nothing in the world is shared between scenarios, which is why parallel runs are safe as long as the *external* data is unique. See [Scenario isolation](/explanations/scenario-isolation/).

## 5. Packs

A **pack** is a bundle of steps, parameter types and hooks, its settings in `axx.yaml` and its tools for agents (`axx mcp`), plus the per-scenario context its steps share. The packs are `rest`, `mock`, `sql`, `mongo`, `kafka`, `amqp`, `mqtt`, `nats`, `websocket`, `sse`, `logs`, `files`, `cli`, the web packs (`web-core`, `web-screenshots`...), the cloud service packs (`aws-s3`, `gcp-pubsub`, `azure-blob`...), and `core` for shared parameter types.

A project lists the packs it uses in `axx-packs.yaml`. The `axx` binary is the core alone: on first use, it builds a copy of itself with the project's packs, caches it and runs it in its place ([Choose packs](/guides/use-packs/)).

## 6. Report and stop

Reporters follow the run as it goes, as [Cucumber Messages](https://github.com/cucumber/messages) events and scenario results, and finish their reports after the last scenario: pretty, progress or compact on the console, TeamCity service messages for the editors' test runners, and JUnit, HTML, Cucumber JSON, NDJSON messages and the JSON agent report to files. Then apps are stopped (signal, grace period, kill) and every `cleanup` runs, including after a crash or an interrupt. The exit code summarizes the worst outcome ([exit codes](/references/error-codes/#exit-codes)).

## What Axx is not

- **Not a unit test framework.** Axx tests a running system through its public interfaces. It never loads your code.
- **Not a load-testing tool.** Parallelism is for speed, not for generating traffic.
