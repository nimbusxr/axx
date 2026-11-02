---
name: axx-setup
description: Adopt axx in a repository - axx init, axx.yaml services and readiness checks, docker compose infrastructure, running in CI (GitHub Actions, GitLab CI), and agent integration (skills, MCP). Use when setting up acceptance testing for a service.
license: Apache-2.0
---

# Setting up axx in a repository

## 1. Initialize

```sh
axx init --dry-run   # what it would write, and the packs the repository points to
axx init             # axx.yaml, axx-packs.yaml (rest), a smoke feature, CI, the AGENTS.md section, the agents' setup
axx pack add sql     # the packs the tests need: init suggests them, `axx pack show <name>` says what one does
axx doctor           # config, the services' commands, Docker, the features
```

`axx init` leaves existing files alone (`--force` overwrites), updates its `AGENTS.md` section in place, and is safe to run again. Its service is a guess to check: the port (8080) and health check (`/health`) in `axx.yaml` and the smoke feature are placeholders, and with a compose file it starts that file as a stack of its own (`-p <name>-axx`). `--no-agents` skips the skills and the MCP server.

## 2. The services

```yaml
services:
  api:
    command: docker compose -p api-axx up --build   # or ./gradlew bootRun, npm start, go run ./cmd/api
    ready:
      http: {url: http://localhost:8080/health}   # 2xx when ready; also tcp, exec, log (a regex)
      timeout: 120s
    cleanup: docker compose -p api-axx down -v --remove-orphans   # always runs, even after a crash
```

- A service is what runs on its own, reached by an address. A program its user starts (a CLI, an MCP server over stdio) is run by its step's `command`, not a service.
- Commands run without a shell: `shell: true` for pipes or `&&`. `dependsOn: [db]` orders services; the others start in parallel. A command that returns before the service is ready (`docker compose up -d`) is fine: readiness is checked until it holds.
- A compose stack gets a name of its own (`-p api-axx`), so that it never takes over the stack a developer runs from the same file, or deletes its volumes.
- Iterate with `axx run`: it starts the services and stops them. `axx up` and `axx down` only keep them running to inspect them.
- Debug the service from an IDE with `axx run --attach api`, or `axx run --debug` with `services.api.debug`.

## 3. CI

```yaml
- uses: nimbusxr/setup-axx@v0
- run: axx lint --format github
- run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
```

Elsewhere: the install script (`curl -fsSL https://raw.githubusercontent.com/nimbusxr/axx/main/scripts/install.sh | sh`) or the image `ghcr.io/nimbusxr/axx`. `axx run --shard i/n` shares a suite between runners; profiles hold CI's differences (`--profile ci`). With a `fixtures` section: `axx fixtures check`, then `axx fixtures generate`, before `axx run`.

## 4. Agents

- `axx init` writes the skills to `.agents/skills` (linked into `.claude/skills` for Claude Code) and connects the axx MCP server for the agents the repository uses, in their own files. `axx mcp install --agent claude|codex|cursor|vscode|gemini` connects one; Codex's servers live in `~/.codex/config.toml`: `axx mcp install --agent codex --scope user`.
- `axx skills install` refreshes the skills after an upgrade.
- An agent whose sandbox keeps it from Docker gets its services from `axx host`, which the person runs on their own machine; runs find it in `.axx/host.json`.
- `axx doctor` warns when an agent lacks the skills or the MCP server.
