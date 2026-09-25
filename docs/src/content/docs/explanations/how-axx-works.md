---
title: How Axx works
description: What happens between typing axx run and reading the result - configuration, step matching, app lifecycle, parallel execution, packs, the world and reporting.
---

Axx is one Go binary with its own Cucumber executor. It reads Gherkin, matches every step to a definition, starts the system under test, runs the scenarios against it from the outside, and reports.

```text
axx.yaml ─┐
features ─┼─▶ load & validate ─▶ match steps ─▶ start apps ─▶ run scenarios ─▶ stop apps ─▶ reporters
packs    ─┘                     (registry)      (lifecycle)   (workers)         (cleanup)    (pretty, junit, json…)
```

## 1. Load

Axx finds `axx.yaml` by walking up from the working directory, applies the selected profile and `axx.local.yaml`, expands `${env:...}` and `${sys:...}`, and validates the result against the [schema](/references/config/). It then parses the feature files with the official Cucumber Gherkin parser and applies the path, line, tag and name filters.

## 2. Match

Every step line is matched against the **registry**: the steps of the packs the project loads. Steps are [Cucumber Expressions](https://github.com/cucumber/cucumber-expressions) with custom parameter types such as `{ordinal}` and `{service}`. Optional segments like `[[ on {service}]]` register every variant of a step.

A line that matches nothing is *undefined*; a line that matches two definitions is *ambiguous*. Both are found before anything runs: `axx validate` and `axx explain` are this phase alone, so they never start an app.

## 3. Start the apps

The lifecycle manager starts `apps` in dependency order, independent apps in parallel, each in its own process group. It polls every `ready` check until they pass or time out. With `active.enabled`, only apps whose tags match the selected scenarios start. With `axx up`, a background supervisor keeps them running and later runs skip this step.

## 4. Run

Scenarios run in parallel on `run.workers` workers; steps within a scenario run in order. Scenarios tagged with a `run.exclusive` tag run one at a time after the parallel phase.

Each scenario gets a fresh **world**: its registered services and the state each pack keeps (requests built and responses received, selections, events). Nothing in the world is shared between scenarios, which is why parallel runs are safe as long as the *external* data is unique. See [Scenario isolation](/explanations/scenario-isolation/).

## 5. Packs

A **pack** is a bundle of steps, parameter types and hooks, plus the per-scenario context its steps share. The packs are `rest`, `mock`, `sql`, `mongo`, `kafka`, `logs`, and `core` for shared parameter types.

## 6. Stop and report

After the last scenario, apps are stopped (signal, grace period, kill) and every `cleanup` runs, including after a crash or an interrupt. Reporters receive the run as [Cucumber Messages](https://github.com/cucumber/messages) events and render them: pretty, progress or compact on the console; JUnit, HTML, Cucumber JSON, NDJSON messages and the JSON agent report to files. The exit code summarizes the worst outcome ([exit codes](/references/error-codes/#exit-codes)).

## What Axx is not

- **Not a unit test framework.** Axx tests a running system through its public interfaces. It never loads your code.
- **Not a load-testing tool.** Parallelism is for speed, not for generating traffic.
