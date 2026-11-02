---
title: Axx for agents
description: The design choices that make Axx usable by coding agents - discoverable steps, validation without side effects, compact structured output, stable contracts, and docs served from the binary.
---

Coding agents are now a primary user of test tools: they write most new tests, run them, and act on the result. Axx is designed for that user alongside people. The principle: an agent should never have to guess.

## Never guess a step

Agents invent plausible step text, and Gherkin punishes near-misses. Axx makes the real steps cheap to find and cheap to check:

- `axx steps` and the `steps_search` MCP tool without a query list every step of the project in one line each, which an agent reads once. A step's line is usually enough to write it: `axx steps show <id>...` and `step_explain` give the documentation and examples of those that leave it unsure, several at once. Searches return the steps that fit, briefly, and the MCP tool sends a step it returned before as its id only, so what an agent learns does not fill its context twice.
- The skills send agents to `axx steps`, which lists the project's steps, custom ones included, rather than shipping pages of them: agents read pages a chunk at a time, about ten times the tokens of `axx steps show <id>`.
- Axx knows every pack it publishes before a project adds it, offline and for the version installed. `axx pack list` says what each one reaches, `axx pack show <name>` gives its settings and steps, `axx steps search <words>` finds steps in all of them, with the pack to add, and a step of a pack the project lacks is undefined with that pack named. `axx init` lists the packs a repository points to.
- `axx validate` and `axx explain` check every line without starting anything, and suggest the closest real steps for a near-miss.
- The `steps_try` MCP tool runs steps in a live scenario that stays open between calls, so an agent sees what a step does before it writes it down; the packs' tools look at that scenario, such as the web-core pack's `web_page`, which shows the page as the steps name its elements.
- Step text is public API: it never changes, so what an agent learned stays true. Some steps also take the other wordings agents write by themselves: those run the step and stay out of the lists, so the list an agent reads stays short.

## Never guess what happened

- **Exit codes** classify the outcome: a failed scenario (`1`), a broken setup (`2`), an invented step (`3`), a service that did not start (`4`).
- **Error codes** (`AXX-Exxxx`) are stable and come with a hint and a link; `axx explain <code>` gives the same page offline, and compact output points to it.
- **Failures are data.** Assertion failures carry expected and actual values, the matched step definition, pack context such as the last HTTP exchange, and the exact command that reruns the scenario.
- **`--json` everywhere**, in one envelope, with a frozen schema.

## Spend tokens on the problem

When Axx detects an agent and its output is not a terminal, it switches to compact output: failures and one summary line. A green run of a thousand scenarios costs one line of context. Details are one call away (`axx run --json`, or the `failure_context` MCP tool). Lookups are short in compact output too: `axx steps show` gives a step's documentation and first example, `axx pack show` a pack's first paragraph, with its steps when the project lacks it, and `axx schema` the outline of its keys. `--full` gives the rest.

## Stay in sync with the installed version

Documentation drifts; binaries do not. The MCP server, `axx steps`, the skills and the reference pages on this site are all generated from the binary (ADR 0005), so an agent that asks the tool gets answers for the version in the repository, including that project's custom packs.

## A fast, safe loop

An agent checks its work with one command: `axx validate && axx lint && axx run --compact`. Validate and lint need no services and stop it at what they find; the run starts the services and stops them. With the MCP tools, the same is `feature_validate`, `lint_run` and `scenarios_run` in one turn. `axx up` and `axx down` only keep the services running to inspect them. `axx run features/x.feature:LINE` reruns exactly one scenario. Validation, explanation and step search have no side effects and need no running services.

## Guardrails in the instructions

The AGENTS.md section and the MCP server's instructions carry the rules that keep agent-written tests honest, and the skills add the details:

- a scenario is one behavior, named after it, and each acceptance criterion is a scenario of its own, even when one action shows several;
- unique data in every scenario, and fixture factories where data repeats (optional, and `axx lint` hints at them);
- assert on observable outcomes, never loosen an assertion to go green;
- no fixed sleeps, use readiness checks and polling steps.

## Docs for machines

Every page on this site has a Markdown twin (append `.md`), advertised with `<link rel="alternate" type="text/markdown">`. [`/llms.txt`](/llms.txt) indexes them, and [`/.well-known/agent-skills/index.json`](/.well-known/agent-skills/index.json) lists the skills. Each page also has *Copy as Markdown* and *Open in Claude* buttons.

`axx init` sets up the skills and the MCP server for the agents a repository already uses, in the repository's own files, and `axx doctor` says what an agent lacks. Read [Set up agents](/guides/set-up-agents/), and see it work in [Test with an agent](/tutorials/with-an-agent/).

How much this helps is measured: [Agent evaluations](/explanations/agent-evals/) runs coding agents on the same tasks with axx and without it, against a service with planted bugs.
