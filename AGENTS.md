# AGENTS.md: working on the axx codebase

axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for
the agentic era.

This file is for agents *contributing to axx*. If you are *using* axx in another repo, install
the skills instead: `axx skills install`.

## Layout

- `cmd/axx`: CLI entry point. `internal/cli`: cobra commands.
- `internal/{config,interp,feature,match,runner,events,report,lifecycle}`: the core runner.
- `core`: the public core every pack builds on (scenario, steps, service registry, state,
  errors, interpolation, shared parameter types).
- `packs/*`: the step packs (rest, mock, sql, mongo, kafka, logs, and `aws/*`, `gcp/*`, `azure/*`
  for cloud services), each with its public context. `internal/packset/catalog.go` lists them.
- `internal/packbuild`, `internal/gotool`: axx builds itself with the packs a project lists in
  `axx-packs.yaml`, using a Go toolchain it downloads itself.
- `packs/all` and `internal/tools/axxall`: axx with every pack, for generated docs and skills,
  tests and the evals image.
- `internal/compat`: the value semantics steps follow (Jayway JSONPath, Java regex, Java number
  formatting; ADR 0007). The oracle tests depend on these; don't "simplify" them.
- `extensions/wiremock-openapi`, `ide/intellij`: JVM parts (the WireMock extension and the IntelliJ plugin).
- `ide/vscode`: the VS Code extension (TypeScript). Both editor clients run `axx lsp` (`internal/lsp`).
- `testdata/steps.json`: the frozen catalog of the step text of axx's packs. `testdata/oracles`:
  recorded behavior oracles. **Never edit either by hand**: they define correct behavior.
- `testdata/openapi-agreement`: exchanges that both OpenAPI validators (the REST pack's and the
  WireMock extension's) must report with the same keys; both test suites read it.
- `evals/`: agent evaluations on Harbor (its own Go module; see `evals/README.md`). They run on
  demand only (`.github/workflows/evals.yml`), never on pull requests.

## Rules

- Step expression text is public API. Never change existing step text; add new steps instead.
- Exit codes (`internal/exitcode`) and the `--json` envelope are frozen contracts (ADR 0003).
- Every user-facing error is an `*axxerr.Error` with a stable `AXX-Exxxx` code and a hint.
- Generated files (step reference, schemas, skills) come from `go generate ./...`; never hand-edit them.
- `cmd/axx` is the core only. Never import a pack from it: every pack, ours included, gets into
  axx through `axx-packs.yaml`, and pack dependencies stay out of the core binary.
- Keep `CGO_ENABLED=0` builds working: pure-Go dependencies only.
- The `hygiene` check (`scripts/hygiene.sh`) must pass; it blocks references to unrelated
  organizations and internal infrastructure.

## Commands

```sh
mise run test          # go test -race ./...
mise run lint          # golangci-lint run
mise run generate      # regenerate code, schemas, docs, skills
mise run hygiene       # denylist + gitleaks
go build -o bin/axx ./cmd/axx                     # the core; prepares a project's packs on first use
go build -o bin/axx-all ./internal/tools/axxall   # every pack built in
```
