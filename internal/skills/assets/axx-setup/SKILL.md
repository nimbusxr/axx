---
name: axx-setup
description: Adopt axx (the human-readable acceptance testing framework) in a repository - axx init, axx.yaml apps and readiness checks, docker compose infrastructure, running in CI (GitHub Actions, GitLab CI), and agent integration (skills, MCP). Use when setting up acceptance testing for a service.
license: Apache-2.0
---

# Setting up axx in a repository

## 1. Initialize

```sh
axx init            # axx.yaml, axx-packs.yaml, features/smoke.feature, .github/workflows/acceptance.yml, AGENTS.md section, .gitignore
axx pack add sql    # the packs whose steps the project uses (init lists rest); `axx pack list` shows them all
axx doctor          # verify: config, apps' commands, docker, features
```

`axx init` detects `compose.yaml` and OpenAPI files. It never overwrites existing files unless you pass `--force`, and it is safe to run again.

## 2. Describe how to start the system under test

```yaml
apps:
  api:
    dir: .
    command: docker compose up --build        # or ./gradlew bootRun, npm start, go run ./cmd/api ...
    ready:
      http: {url: http://localhost:8080/health}   # every URL must return 2xx; also: tcp, exec, log (regex)
      timeout: 120s
    cleanup: docker compose down -v --remove-orphans   # always runs, even if the app crashed
    active: {tags: ["@api"]}                  # with active.enabled: start only when selected scenarios need it
```

- Commands run without a shell. Set `shell: true` if a command needs pipes or `&&`.
- Order apps with `dependsOn: [db]`. Independent apps start in parallel.
- A command that exits 0 before the app is ready (`docker compose up -d`) is fine: axx keeps checking readiness.
- For fast local iteration, run `axx up` once, then `axx run` as often as you like, then `axx down`.
- To debug the app, run it yourself from your IDE with `axx run --attach api`, or use `axx run --debug` with `apps.api.debug`.

## 3. CI

GitHub Actions:

```yaml
- uses: nimbusxr/setup-axx@v1
- run: axx lint --format github --format sarif:build/axx/lint.sarif   # test-data isolation, annotated in the PR
- run: axx run --format junit:build/axx/junit.xml --format html:build/axx/report.html
```

Anywhere else, install axx with `curl -fsSL https://axx.nimbusxr.us/install.sh | sh` or use the image `ghcr.io/nimbusxr/axx`.

- Use profiles for CI-only differences: `profiles: {ci: {properties: {local.host: docker}}}` together with `--profile ci` or `AXX_PROFILE=ci`.
- If `axx.yaml` has a `fixtures` section, run `axx fixtures check` before `axx run`: it fails (exit 1) when committed fixtures drifted from their factory specs. When outputs are `ignored`, run `axx fixtures generate` first to materialize them.
- Exit codes: 0 pass, 1 failures, 2 config, 3 undefined steps or lint violations, 4 app startup.
- `axx lint` checks the test-data isolation rules under `lint:` in `axx.yaml` (values such as seed ids that must be unique across files). Its formats are `human`, `json`, `junit`, `sarif` and `github`; `mode: warn` reports without failing while you adopt a rule.

## 4. Agents

- `axx skills install` adds these skills for Claude Code, Codex, Cursor, Gemini CLI and Copilot.
- `axx mcp` is an MCP server. Add `{"mcpServers": {"axx": {"command": "axx", "args": ["mcp"]}}}` to `.mcp.json`.
- The `AGENTS.md` section written by `axx init` gives any agent the essential rules.

## When a command fails

Errors carry a code (`AXX-E0102`) and usually a hint. Run `axx explain <code>` or read `references/error-codes.md`. Run `axx doctor` to check the environment.
