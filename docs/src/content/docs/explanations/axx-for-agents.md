---
title: Axx for agents
description: The design choices that make Axx usable by coding agents - discoverable steps, validation without side effects, compact structured output, stable contracts, and docs served from the binary.
---

Coding agents are now a primary user of test tools: they write most new tests, run them, and act on the result. Axx is designed for that user alongside people. The principle: an agent should never have to guess.

## Never guess a step

Agents invent plausible step text, and Gherkin punishes near-misses. Axx makes the real steps cheap to find and cheap to check:

- `axx steps search "<intent>"` and the `steps_search` MCP tool return real expressions with docs and complete examples.
- The skills ship a one-line-per-step index generated from the project, including custom steps.
- `axx validate` and `axx explain` check every line without starting anything, and suggest the closest real steps for a near-miss.
- Step text is public API: it never changes, so what an agent learned stays true.

## Never guess what happened

- **Exit codes** classify the outcome: a failed scenario (`1`), a broken setup (`2`), an invented step (`3`), an app that did not start (`4`).
- **Error codes** (`AXX-Exxxx`) are stable and come with a hint and a link.
- **Failures are data.** Assertion failures carry expected and actual values, the matched step definition, pack context such as the last HTTP exchange, and the exact command that reruns the scenario.
- **`--json` everywhere**, in one envelope, with a frozen schema.

## Spend tokens on the problem

When Axx detects an agent and its output is not a terminal, it switches to compact output: failures and one summary line. A green run of a thousand scenarios costs one line of context. Details are one call away (`axx run --json`, or the `failure_context` MCP tool).

## Stay in sync with the installed version

Documentation drifts; binaries do not. The MCP server, `axx steps`, the skills and the reference pages on this site are all generated from the binary (ADR 0005), so an agent that asks the tool gets answers for the version in the repository, including that project's custom packs.

## A fast, safe loop

`axx up` keeps the system under test running between runs, so an agent's edit-run loop costs seconds instead of minutes. `axx run features/x.feature:LINE` reruns exactly one scenario. Validation, explanation and step search have no side effects and need no running apps.

## Guardrails in the instructions

The skills and the AGENTS.md section encode the rules that keep agent-written tests honest:

- one scenario per acceptance criterion, named after the behavior;
- unique data in every scenario;
- assert on observable outcomes, never loosen an assertion to go green;
- no fixed sleeps, use readiness checks and polling steps.

## Docs for machines

Every page on this site has a Markdown twin (append `.md`), advertised with `<link rel="alternate" type="text/markdown">`. [`/llms.txt`](/llms.txt) indexes them, and [`/.well-known/agent-skills/index.json`](/.well-known/agent-skills/index.json) lists the skills. Each page also has *Copy as Markdown* and *Open in Claude* buttons.

Set it up with [Set up agents](/guides/set-up-agents/), and see it work in [Test with an agent](/tutorials/with-an-agent/).
