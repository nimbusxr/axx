---
name: axx-setup
description: Adopt axx in a repository - axx init, axx.yaml services and readiness checks, docker compose infrastructure, running in CI (GitHub Actions, GitLab CI), and agent integration (skills, MCP). Use when setting up acceptance testing for a service.
license: Apache-2.0
---

# Setting up Axx in a repository

## 1. Initialize

```sh
axx init            # axx.yaml, axx-packs.yaml, features/smoke/smoke.feature, .github/workflows/acceptance.yml, AGENTS.md section, .gitignore, .gitattributes, agents
axx pack add sql    # the packs whose steps the project uses (init lists rest); `axx pack list` shows them all and what each reaches, `axx pack show <name>` one's steps
axx doctor          # verify: config, services' commands, docker, features
```

`axx init` detects `compose.yaml` and OpenAPI files. Its service is a guess to check before the first run: with a compose file it starts that file as a stack of its own (`-p <name>-axx`), whose cleanup (`down -v`) removes that stack's volumes, never those of the stack the developer runs from the same file; the port (8080) and health check (`/health`), in `axx.yaml` and the smoke feature, are placeholders. When the compose file runs only what the service needs (a database, object storage) and the service itself runs another way (`npm run dev`, `go run ./cmd/api`), give each its own service, or start just the service against what already runs, on a port of its own: its scenarios' data then sits beside the developer's, which unique data per scenario allows. It leaves existing files alone unless you pass `--force`, adds or updates its `AGENTS.md` section and the `.axx/` line of `.gitignore` in place, and is safe to run again. It also installs the skills in `.agents/skills`, marked generated in `.gitattributes` so their diffs collapse in pull requests, and connects the axx MCP server to the agents the repository already uses, in their own files (see "4. Agents"); `--no-agents` skips that.

## 2. Describe how to start the system under test

```yaml
services:
  api:
    dir: .
    command: docker compose -p api-axx up --build   # or ./gradlew bootRun, npm start, go run ./cmd/api ...
    ready:
      http: {url: http://localhost:8080/health}   # every URL must return 2xx; also: tcp, exec, log (regex)
      timeout: 120s
    cleanup: docker compose -p api-axx down -v --remove-orphans   # always runs, even if the service crashed
    active: {tags: ["@api"]}                  # with active.enabled: start only when selected scenarios need it
```

- A service is what runs on its own and is reached by an address. A program its user starts, like a command-line tool or an MCP server its clients run over stdio, is not a service: the step that uses it runs it (the cli and mcp packs' `command`).
- A compose stack axx starts gets a name of its own (`-p api-axx`): without one, Compose names it after the folder and takes over the stack the developer runs from the same file, and `down -v` deletes that stack's volumes, their local data with them.
- Commands run without a shell. Set `shell: true` if a command needs pipes or `&&`.
- Order services with `dependsOn: [db]`. Independent services start in parallel.
- A command that exits 0 before the service is ready (`docker compose up -d`) is fine: Axx keeps checking readiness.
- For fast local iteration, run `axx up` once, then `axx run` as often as you like, then `axx down`.
- To debug the service, run it yourself from your IDE with `axx run --attach api`, or use `axx run --debug` with `services.api.debug`.

## 3. CI

GitHub Actions:

```yaml
- uses: nimbusxr/setup-axx@v0
- run: axx lint --format github --format sarif:build/axx/lint.sarif   # test-data isolation, annotated in the PR
- run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
```

Anywhere else, install Axx with `curl -fsSL https://axx.nimbusxr.us/install.sh | sh` or use the image `ghcr.io/nimbusxr/axx`.

- A long suite shares a run between runners: a matrix of `axx run --shard i/n` (1 to n) runs each scenario once between them, every nth in their files' and lines' order.
- Use profiles for CI-only differences: `profiles: {ci: {properties: {local.host: docker}}}` together with `--profile ci` or `AXX_PROFILE=ci`.
- If `axx.yaml` has a `fixtures` section, run `axx fixtures check` and then `axx fixtures generate` before `axx run`: check fails (exit 1) when fixtures drifted from their factory specs or the committed manifest is stale, and passes on a fresh clone whose ignored outputs are not generated yet; generate then writes them. Check first: generate would bring a stale manifest up to date and hide it.
- Exit codes: 0 pass, 1 failures, 2 config, 3 undefined steps or lint violations, 4 service startup.
- `axx lint` checks the test-data isolation rules under `lint:` in `axx.yaml` (values such as seed ids that must be unique across files). Its formats are `human`, `json`, `junit`, `sarif` and `github`; `mode: warn` reports without failing while you adopt a rule.

## 4. Agents

- `axx init` sets agents up in the repository only, and lists every file it writes: the skills in `.agents/skills` (linked into `.claude/skills` when the repository uses Claude Code), and the axx MCP server for each agent the repository shows signs of using: `.claude/`, `CLAUDE.md` or `.mcp.json` → `.mcp.json`; `.codex/` → `.codex/config.toml`; `.cursor/` → `.cursor/mcp.json`; `.vscode/` → `.vscode/mcp.json`; `.gemini/` or `GEMINI.md` → `.gemini/settings.json`. Other servers and settings in those files stay; a file it cannot read back exactly (comments, say) is left alone.
- `axx mcp install --agent claude|codex|cursor|vscode|gemini` connects one agent (`--scope user` for your home directory; the default is the project). Codex usually reads `~/.codex/config.toml`, which `axx init` never writes: run `axx mcp install --agent codex --scope user`.
- `axx skills install` installs or refreshes the skills (after upgrading Axx, or adding steps).
- An agent whose sandbox keeps it from Docker gets its services from `axx host`, which the person starts in the project on their own machine (or their editor does). `axx run`, `axx up` and the MCP server find it (`.axx/host.json`) and ask it for the services: it starts them clean for each run and cleans up after; the scenarios run in the sandbox. A run whose services or compose files changed waits for the person's approval.
- `axx doctor` warns when an agent the project uses lacks the skills or the MCP server, with the command that fixes it.
- The `AGENTS.md` section written by `axx init` gives any agent the essential rules. Claude Code reads `CLAUDE.md` instead of `AGENTS.md` wherever a `CLAUDE.md`, `.claude/CLAUDE.md` or `CLAUDE.local.md` is in the folder or one above it: in a repository that uses Claude Code, `axx init` then imports `AGENTS.md` into the project's `CLAUDE.md` (`@AGENTS.md`), so that it reads the section.

## When a command fails

Errors carry a code (`AXX-E0102`) and usually a hint. Run `axx explain <code>` or read `references/error-codes.md`. Run `axx doctor` to check the environment.
