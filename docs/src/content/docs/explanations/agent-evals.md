---
title: Agent evaluations
description: How coding agents do with axx and without it, on the same tasks, held to the same checks, with bugs planted in the service.
---

<!-- Written by `go run ./cmd/evals page` in nimbusxr/axx-evals from the runs' result files; do not edit by hand. -->

Coding agents write acceptance tests for a parcels service (REST and OpenAPI, PostgreSQL, MongoDB, Kafka, a downstream service mocked with WireMock), from acceptance criteria, as a developer would ask them to. Each task runs under five conditions: with axx and none of its aids (`none`), with its agent skills (`skills`), its MCP server (`mcp`), both (`both`), and without axx at all, tests of any kind run by a script (`plain`).

Every suite is held to the same **core checks**: it passes against the correct service, twice and against variants that differ only where the contract allows, and it fails against every bug planted in the service. A suite that passes them all would catch those bugs in a real project. Everything is public in [nimbusxr/axx-evals](https://github.com/nimbusxr/axx-evals): the tasks, the planted bugs, the verifier, and every run's transcripts.

## openai/gpt-6-luna

2026-10-03, axx 0.1.12, opencode through OpenRouter ([the run](https://github.com/nimbusxr/axx-evals/actions/runs/37149658056)).

### With axx and without

The 6 tasks that have a version without axx (`kafka-registered-event`, `mock-address-check`, `mongo-tracking-view`, `openapi-reject-invalid`, `rest-crud-happy-path`, `sql-manifest-import`), each condition's suites held to the same core checks: pass against the correct service twice and against its correct variants, and fail against every planted bug.

**With axx, 72 of 72 suites passed every core check (100%)**, across none, skills, mcp, both; **without axx, 14 of 18 (78%)**. Without axx: 3 failed against the correct service, 1 failed against a correct variant. The agents wrote a median of 76 lines per suite with axx and 170 without.

| On these tasks | none | skills | mcp | both | plain (no axx) |
| --- | --- | --- | --- | --- | --- |
| **Passed every core check** | **18/18** | **18/18** | **18/18** | **18/18** | **14/18** |
| Failed against the correct service | 0 | 0 | 0 | 0 | 3 |
| Failed against a correct variant | 0 | 0 | 0 | 0 | 1 |
| Lines written per suite (median) | 60 | 86 | 80 | 83 | 170 |
| Agent minutes per trial | 1.0 | 1.2 | 1.3 | 1.3 | 1.3 |
| Requests learning axx per trial | 4.4 | 3.2 | 4.2 | 3.6 | 0.0 |
| Cost per trial (¢) | 1.08 | 1.45 | 1.01 | 1.23 | 0.86 |
| **Cost per passing suite (¢)** | **1.08** | **1.45** | **1.01** | **1.23** | **1.10** |

Lines written are the lines of the files the agent added or changed: features and seeds with axx, test code and scripts without. Requests learning axx are those whose tool calls mostly read its steps, docs, skills or help. The cost per passing suite is what every trial cost, divided by the suites that passed every core check. The run's report lists every failed suite and why.

### Scores by task

The share of each task's trials that passed every check; the core score holds every condition to the same checks, and the score adds what only an axx suite has (`axx validate`, the features' readability, `axx lint` where the task asks).

| Task | none | skills | mcp | both | plain |
| --- | --- | --- | --- | --- | --- |
| `fix-broken-feature` | 3/3 | 3/3 | 3/3 | 3/3 | - |
| `init-first-feature` | 3/3 | 3/3 | 3/3 | 3/3 | - |
| `kafka-registered-event` | 3/3 | 3/3 | 3/3 | 3/3 | 0/3 |
| `mock-address-check` | 3/3 | 3/3 | 3/3 | 3/3 | 2/3 |
| `mongo-tracking-view` | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| `openapi-reject-invalid` | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| `parallel-unique-data` | 3/3 | 3/3 | 3/3 | 3/3 | - |
| `rest-crud-happy-path` | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| `sql-manifest-import` | 3/3 | 3/3 | 3/3 | 3/3 | 3/3 |
| **Core score** | **100.0** | **100.0** | **100.0** | **100.0** | **77.8** |
| **Score** | **100.0** | **100.0** | **100.0** | **100.0** | **77.8** |
