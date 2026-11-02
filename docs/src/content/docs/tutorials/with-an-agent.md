---
title: Test with an agent
description: Hand an acceptance criterion to a coding agent, watch it write and run the scenario with Axx, and review what it wrote.
sidebar:
  order: 3
---

In this tutorial, we'll give a coding agent an acceptance criterion in plain words and watch it write a scenario, check it and run it with Axx. Then we'll review what it wrote.

We'll use [Claude Code](https://claude.com/claude-code) and the `hello-axx` project from the [Quickstart](/tutorials/quickstart/). You'll need both.

## Teach the agent Axx

`axx init` installed Axx's skills in `.agents/skills` in the Quickstart. The `hello-axx` directory had no sign of Claude Code then, so connect it now. In the `hello-axx` directory, give Claude Code the Axx MCP server and link the skills where it reads them:

```sh
axx mcp install --agent claude
axx skills install
```

```console
create .mcp.json: the axx MCP server for Claude Code
installed 5 skills to .agents/skills (0 files updated)
linked for Claude Code in .claude/skills
```

The `AGENTS.md` that `axx init` wrote tells an agent how to find Axx's steps, write a scenario, check it and run it. The MCP server gives it tools to do so, and the skills add the details: the shape of a feature, test data, reading a failure. In a repository that already uses Claude Code, `axx init` does all of this itself.

## Ask for a test

Start Claude Code in the project:

```sh
claude
```

It asks whether to use the `axx` server from `.mcp.json`; allow it.

Give it the criterion, not step text:

```text
Add an acceptance test for this: asking for a file that doesn't exist returns 404. Use axx, and run it.
```

## Watch what it does

The agent's wording will differ from run to run, but you'll see it work through the same loop. Claude Code asks before each command and each tool call, so you can follow along. Notice that it:

1. reads the project's steps with the `steps_search` tool (or `axx steps`) instead of guessing their text;
2. looks up only the steps whose line leaves it unsure, several at once, with `step_explain` (`axx steps show`);
3. writes a feature file, with a scenario for each acceptance criterion;
4. checks the features and runs them in one go, with `feature_validate`, `lint_run` and `scenarios_run` (`axx validate && axx lint && axx run --compact`). The run starts the server and stops it.

## Review what it wrote

Open the feature file the agent created. In our run it was `features/missing-file.feature`:

```gherkin title="features/missing-file.feature"
Feature: Missing files
  Asking for a file the service does not have is answered with Not Found.

  Background:
    Given the hello-axx service with the following properties:
      | url | http://${sys:local.host}:8000 |

  Scenario: Asking for a file that does not exist returns 404
    Given a GET request to /no-such-file-404.json
    When the request is executed
    Then the response status code is 404
```

Read it against the criterion you gave. Each line says what it checks, so you can tell at a glance whether the agent tested what you asked for. That review is the part that stays yours.

## Run it yourself

```sh
axx run
```

The end of the output shows both scenarios, the Quickstart's and the agent's:

```console
2 scenarios (2 passed)
9 steps (9 passed)
```

## What you've done

You gave an agent a criterion in plain words. It found the steps, wrote the scenario, checked it and ran it, and you confirmed it by reading it.

[Set up agents](/guides/set-up-agents/) connects other agents and Axx's MCP server, and [Axx for agents](/explanations/axx-for-agents/) explains why Axx works this way.
