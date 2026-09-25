---
title: Set up agents
description: Install the Axx skills, connect the Axx MCP server to Claude Code, Cursor, Codex or VS Code, and keep the AGENTS.md section current.
---

Three pieces give a coding agent everything it needs. Install all three; they reinforce each other. [Test with an agent](/tutorials/with-an-agent/) shows them in use.

## Skills

```sh
axx skills install                 # this repository
axx skills install --scope user    # your home directory, for every repository
axx skills list
```

`install` writes the skills to `.agents/skills/` (read by Codex, Cursor, Gemini CLI and Copilot) and links them into `.claude/skills/` for Claude Code (`--no-claude` skips the link). Commit them so every contributor and every CI agent gets them.

| Skill | Teaches |
| --- | --- |
| `axx-acceptance-tests` | the write, validate, run loop; rules that keep tests reliable; the step index |
| `axx-setup` | `axx init`, apps and readiness, CI, agent integration |
| `axx-custom-steps` | custom steps as Go packs |
| `axx-debugging` | reading failures, exit codes and logs |

The step references inside the skills are generated from *your* project, including your custom packs. Rerun `axx skills install` after adding steps or upgrading Axx; files you edited are kept unless you pass `--force`. The same skills are published at [`/.well-known/agent-skills/index.json`](/.well-known/agent-skills/index.json).

## MCP server

`axx mcp` serves Axx over the [Model Context Protocol](https://modelcontextprotocol.io) on stdio. Because it is the installed binary, its answers match your Axx version and your project's steps.

| Tool | Does |
| --- | --- |
| `steps_search` | find steps by intent, with docs and examples |
| `step_explain` | how one line matches, or the closest steps |
| `feature_validate` | check feature files or feature text without running |
| `scenarios_run` | run scenarios (paths, tags, names); returns failures with expected and actual |
| `failure_context` | logs, attachments and the last request and response of one failure |
| `env` | `up`, `down` or `status` of the apps |
| `config_show` | the effective `axx.yaml`, with secrets redacted |
| `scaffold` | starter contents for a feature or an `axx.yaml` |

It also serves the `axx.yaml` JSON Schema as a resource and a `write-acceptance-tests` prompt. Pass `--profile ci` (or any profile) to apply it to every tool call.

### Claude Code

```json title=".mcp.json"
{
  "mcpServers": {
    "axx": { "command": "axx", "args": ["mcp"] }
  }
}
```

The same from the command line: `claude mcp add axx -- axx mcp`. Or install the skills and the server together as a plugin:

```text
/plugin marketplace add nimbusxr/axx
/plugin install axx@nimbusxr
```

### Cursor

```json title=".cursor/mcp.json"
{
  "mcpServers": {
    "axx": { "command": "axx", "args": ["mcp"] }
  }
}
```

### Codex

```toml title="~/.codex/config.toml"
[mcp_servers.axx]
command = "axx"
args = ["mcp"]
```

### VS Code (GitHub Copilot)

```json title=".vscode/mcp.json"
{
  "servers": {
    "axx": { "type": "stdio", "command": "axx", "args": ["mcp"] }
  }
}
```

## AGENTS.md

`axx init` adds a managed section to `AGENTS.md`, the file most agents read first. Running `axx init` again updates the section in place and leaves the rest of the file alone:

```markdown title="AGENTS.md"
<!-- axx:begin (managed by `axx init`; edit outside this block) -->
## Acceptance tests (axx)
axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework.
- Feature files are acceptance criteria a person can read: one scenario per criterion, in plain
  language, using the steps exactly as written. No programming constructs in Gherkin.
- Find steps before writing: `axx steps search "<intent>"`; never invent step text.
- Steps come from the packs in `axx-packs.yaml`; `axx pack list` shows the others, `axx pack add <name>` adds one.
- Check without running: `axx validate`. Explain one line: `axx explain "<step>"`.
- Check test data: `axx lint` reports ids and keys that collide across seed and fixture files.
- Run: `axx up` once (keeps apps running), then `axx run --compact`; `axx down` when done.
- Every scenario uses unique data (IDs, names, keys): scenarios run in parallel and data persists.
- Features live in `features/`; configuration in `axx.yaml` (schema: `axx schema`).
- Diagnose failures from the report: `axx run --json` includes expected/actual and a rerun command.
<!-- axx:end -->
```

## Docs for agents

- Every page on this site has a Markdown twin: append `.md` to the path (`/guides/set-up-agents.md`). Pages advertise it with `<link rel="alternate" type="text/markdown">`.
- [`/llms.txt`](/llms.txt) indexes the site; [`/llms-full.txt`](/llms-full.txt) and [`/llms-small.txt`](/llms-small.txt) contain it in one file.
- The `axx.yaml` schema is at [`/schemas/v0/axx.schema.json`](/schemas/v0/axx.schema.json); `axx schema` prints the same thing offline.
