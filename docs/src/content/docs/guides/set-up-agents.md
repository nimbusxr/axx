---
title: Set up agents
description: Give coding agents the Axx skills, the Axx MCP server and the AGENTS.md section. axx init sets them up for the agents a repository uses, axx mcp install connects one more, and axx doctor checks them.
---

Three pieces give a coding agent everything it needs: the skills, the MCP server and the `AGENTS.md` section. They reinforce each other, and `axx init` sets up all three for the agents the repository already uses. [Test with an agent](/tutorials/with-an-agent/) shows them in use.

## What axx init sets up

`axx init` works in the repository only and lists every file it writes. It never writes to your home directory.

| When the repository has | `axx init` writes |
| --- | --- |
| anything | the `AGENTS.md` section and the skills in `.agents/skills/` |
| `.claude/`, `CLAUDE.md` or `.mcp.json` (Claude Code) | `.mcp.json`, and links the skills into `.claude/skills/` |
| `.codex/` (Codex) | `.codex/config.toml` |
| `.cursor/` (Cursor) | `.cursor/mcp.json` |
| `.vscode/` (VS Code) | `.vscode/mcp.json` |
| `.gemini/` or `GEMINI.md` (Gemini CLI) | `.gemini/settings.json` |

In a repository with a `CLAUDE.md` and a `.cursor/` directory:

```console
  create  axx.yaml
  create  axx-packs.yaml
  create  features/smoke.feature
  create  .github/workflows/acceptance.yml
  create  AGENTS.md
  create  .gitignore
  create  .gitattributes
  create  .agents/skills (5 skills)
  create  .claude/skills (linked for Claude Code)
  create  .mcp.json (the axx MCP server for Claude Code)
  create  .cursor/mcp.json (the axx MCP server for Cursor)

Codex keeps its MCP servers in ~/.codex/config.toml; `axx mcp install --agent codex --scope user` adds axx there.
next: edit apps in axx.yaml, then `axx doctor` and `axx run`
```

Axx adds its server to the files that exist: other servers and settings stay where they are, and a file that has the axx server already is left alone, so running `axx init` again changes nothing. A file Axx cannot read back exactly, such as JSON with comments, is left alone too: its line says so, and `axx mcp install --agent <agent>` shows what to add by hand. Codex usually reads its servers from your home directory, which `axx init` never writes, so it prints the command that does. `axx init --no-agents` skips the skills and the MCP servers.

Commit what `axx init` wrote, so every contributor and every CI agent gets it.

## Skills

```sh
axx skills install                 # this repository
axx skills install --scope user    # your home directory, for every repository
axx skills list
```

`install` writes the skills to `.agents/skills/` (read by Codex, Cursor, Gemini CLI and Copilot) and, when the repository uses Claude Code (a `.claude/` directory, `CLAUDE.md` or `.mcp.json`), links them into `.claude/skills/` for it. `--claude` links them anyway, and `--no-claude` never; with `--scope user`, they are linked when your home directory has `.claude/`. A `.claude/` that holds nothing but these links does not count as using Claude Code, so `axx doctor` does not ask a Codex repository for a Claude Code MCP server. `axx init` installs them the same way, and marks them as generated in `.gitattributes`, so GitHub collapses their diffs in pull requests. Commit them so every contributor and every CI agent gets them.

| Skill | Teaches |
| --- | --- |
| `axx-acceptance-tests` | the write, validate, run loop; rules that keep tests reliable; the step index |
| `axx-test-data` | seeds, payloads and mock bodies in [fixture factories](/guides/fixture-factories/): `axx fixtures`, identities, adoption, `axx lint`, CI |
| `axx-setup` | `axx init`, apps and readiness, CI, agent integration |
| `axx-custom-steps` | custom steps as Go packs |
| `axx-debugging` | reading failures, exit codes and logs |

The skills carry no pages of steps: they send agents to `axx steps`, which lists your project's steps, your custom packs' included, a line each, and to `axx steps show <id>` for one step's documentation. Agents read pages a chunk at a time, about ten times the tokens. Rerun `axx skills install` after upgrading Axx; files you edited are kept unless you pass `--force`, and files the skills no longer have are removed. The same skills are published at [`/.well-known/agent-skills/index.json`](/.well-known/agent-skills/index.json).

## MCP server

`axx mcp` serves Axx over the [Model Context Protocol](https://modelcontextprotocol.io) on stdio. Because it is the installed binary, its answers match your Axx version and your project's steps.

| Tool | Does |
| --- | --- |
| `steps_search` | without a query, the catalog: every step of the project, one line each; with one, the steps that fit it (id, expression, table columns and an example; 6 by default), a step it returned before as its id only |
| `step_explain` | with a line, how it matches (or the closest steps: first the line written out when it keeps an expression's notation, or with its ordinal moved where the step has it); with an id or several, the steps' documentation and examples |
| `feature_validate` | check feature files or feature text without running; warnings come from `axx lint`'s feature checks (a scenario that checks several behaviors in turn among them), and hints name scenarios whose checks prove little |
| `lint_run` | run `axx lint`: values such as seed ids that collide across files, with `file:line`, and the rules that found something |
| `scenarios_run` | run scenarios (paths, tags, names); returns failures with expected and actual, the warnings `feature_validate` and `lint_run` would give, and for a passing run their hints |
| `failure_context` | logs, attachments and the last request and response of one failure; without a run ID, of the latest run |
| `env` | `up`, `down` or `status` of the apps: which are running, left over from a killed run, or not cleaned up (`axx env` on the command line) |
| `config_show` | the effective `axx.yaml`, with secrets redacted, and its packs (`axx config show`) |
| `scaffold` | starter contents for a feature or an `axx.yaml` |
| `steps_try` | try steps in a live scenario that stays open between calls, until `restart` |

Packs add tools of their own, which look at the scenario `steps_try` keeps open. The `web-core` pack's:

| Tool | Does |
| --- | --- |
| `web_page` | the page the session's web app is on: its elements as steps name them, what a screen reader reads, script errors and a screenshot |

An agent tries the steps it is unsure of, looks at the page they led to, then writes them into the feature:

```json
{"steps": "Given the portal web app with the following properties:\n  | url | http://localhost:8400/portal |\nWhen the \"/quote\" page is opened"}
```

The apps must be running (`env` `up`). The session's browsers close when the agent restarts it or the server stops.

It also serves the `axx.yaml` JSON Schema as a resource and a `write-acceptance-tests` prompt. Pass `--profile ci` (or any profile) to apply it to every tool call.

### Connect an agent

`axx mcp install --agent <agent>` adds the server to one agent's configuration: in the repository by default, or with `--scope user` in your home directory, which only this command writes, and only when you ask. Like `axx init`, it keeps what the file holds and leaves a file that has axx alone. `--dry-run` shows what it would write.

```sh
axx mcp install --agent cursor                # .cursor/mcp.json
axx mcp install --agent codex --scope user    # ~/.codex/config.toml
```

| `--agent` | `--scope project` (the default) | `--scope user` |
| --- | --- | --- |
| `claude` (Claude Code) | `.mcp.json` | prints `claude mcp add --scope user axx -- axx mcp` |
| `codex` (Codex) | `.codex/config.toml` | `~/.codex/config.toml` (`$CODEX_HOME/config.toml` when set) |
| `cursor` (Cursor) | `.cursor/mcp.json` | `~/.cursor/mcp.json` |
| `vscode` (VS Code, GitHub Copilot) | `.vscode/mcp.json` | prints `code --add-mcp '{"name":"axx","command":"axx","args":["mcp"]}'` |
| `gemini` (Gemini CLI) | `.gemini/settings.json` | `~/.gemini/settings.json` |

Claude Code keeps your own servers with the rest of its state, and VS Code in its profile, so for them Axx prints the agent's own command instead of editing those files. Codex loads a project's `.codex/config.toml` only once you trust the project.

This is what each agent's file gets.

#### Claude Code

```json title=".mcp.json"
{
  "mcpServers": {
    "axx": { "command": "axx", "args": ["mcp"] }
  }
}
```

Claude Code asks before it starts a server from a project's `.mcp.json`. Or install the skills and the server together as a plugin:

```text
/plugin marketplace add nimbusxr/axx
/plugin install axx@nimbusxr
```

#### Codex

```toml title=".codex/config.toml or ~/.codex/config.toml"
[mcp_servers.axx]
command = "axx"
args = ["mcp"]
```

#### Cursor

```json title=".cursor/mcp.json"
{
  "mcpServers": {
    "axx": { "command": "axx", "args": ["mcp"] }
  }
}
```

#### VS Code (GitHub Copilot)

```json title=".vscode/mcp.json"
{
  "servers": {
    "axx": { "type": "stdio", "command": "axx", "args": ["mcp"] }
  }
}
```

#### Gemini CLI

```json title=".gemini/settings.json"
{
  "mcpServers": {
    "axx": { "command": "axx", "args": ["mcp"] }
  }
}
```

## Check the setup

`axx doctor` checks, for the agents the repository uses (and Codex, when your machine has it), that the skills are installed and that the agent finds the axx MCP server, in the repository's file, your own, or (Claude Code) the plugin. What is missing is a warning with the command that fixes it:

```console
ok   agent skills             5 skills
ok   Claude Code skills       5 skills
ok   Claude Code MCP          axx server in .mcp.json
warn Cursor MCP               no axx server in .cursor/mcp.json
                              → `axx mcp install --agent cursor` adds it
```

## AGENTS.md

`axx init` adds a managed section to `AGENTS.md`, the file most agents read first. Running `axx init` again updates the section in place and leaves the rest of the file alone:

```markdown title="AGENTS.md"
<!-- axx:begin (managed by `axx init`; edit outside this block) -->
## Acceptance tests (axx)
axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework.
- Writing tests: `axx steps` lists every step, a line each (read it once; `axx steps show <id>` gives one
  step's documentation); write one scenario per acceptance criterion with those steps, never
  invented ones, written out (a {name} replaced with a value, a(n) as "a" or "an", [[...]] kept
  without the brackets or left out); `axx up` once, then `axx run --compact`, which also reports what validate and lint find;
  `axx down` when done.
- Use the axx MCP tools when you have them: `steps_search` without a query is the step list, then
  `scenarios_run` (it reports what `feature_validate` and `lint_run` find) and `failure_context`.
- If `.agents/skills` has the axx skills, they teach the details: axx-acceptance-tests, axx-test-data
  (fixture factories), axx-debugging. `axx skills install` installs or updates them.
- Feature files are acceptance criteria a person can read, in plain language, with the steps as
  written. No programming constructs in Gherkin.
- Every scenario uses unique data (ids, names, keys): scenarios run in parallel and data persists.
- Check what the service did (a response property, a row, an event) and why it refused, not only
  status codes.
- Steps come from the packs in `axx-packs.yaml` (`axx pack list`, `axx pack add <name>`). Configuration is in
  `axx.yaml` (`axx schema --outline` lists its keys); features in `features/`.
- Repeating payloads, seeds or mock bodies: fixture factories generate them (`axx fixtures`;
  optional, strongly recommended where data repeats); `axx fixtures adopt` turns hand-written ones into one.
<!-- axx:end -->
```

## Docs for agents

- Every page on this site has a Markdown twin: append `.md` to the path (`/guides/set-up-agents.md`). Pages advertise it with `<link rel="alternate" type="text/markdown">`.
- [`/llms.txt`](/llms.txt) indexes the site; [`/llms-full.txt`](/llms-full.txt) and [`/llms-small.txt`](/llms-small.txt) contain it in one file.
- The `axx.yaml` schema is at [`/schemas/v0/axx.schema.json`](/schemas/v0/axx.schema.json); `axx schema` prints the same thing offline.
