# AGENTS.md: working on the axx codebase

axx (github.com/nimbusxr/axx, "axxeptance") is a human-readable acceptance testing framework for
the agentic era.

This file is for agents *contributing to axx*. If you are *using* axx in another repo, install
the skills instead: `axx skills install`.

## Layout

- `cmd/axx`: CLI entry point; it calls `app.Main`, as the builds with a project's packs do.
  `internal/cli`: cobra commands.
- `internal/{config,interp,feature,match,engine,runner,report,lifecycle}`: the core runner.
- `core`: the public core every pack builds on (scenario, steps, service registry, state,
  errors, interpolation, shared parameter types).
- `packs/*`: the step packs (rest, mock, sql, mongo, kafka, logs, files; `web/*`, `aws/*` and
  `gcp/*`: a core pack and the packs that build on it; `azure/*`), each with its public context. `internal/packset/catalog.go` lists them.
  Packs that build on another (`aws-s3` on `aws-core`, `web-screenshots` on `web-core`) declare it in
  `Requires` and use its public context (`packs/web/core/public.go`); the core stays free of any
  pack's specifics: packs get what they need through extension points in `core`. A pack's
  manifest gives its steps, parameter types, hooks, settings and `axx mcp` tools (`Tools`, which
  look at the scenario an agent keeps open with `steps_try`; `internal/runner/session.go`).
- `packs/web/core` (`web-core`): drives Playwright through playwright-go, with the browsers on the machine that runs
  axx. It pins the Playwright version, the Node.js version and their hashes in
  `packs/web/internal/driver`; upgrading playwright-go means updating them
  (`TestPinnedPlaywrightVersion` fails until they agree). The driver it prepares is patched
  (`packs/web/internal/driver/patch.go`, `patches/*.js`) so that Playwright's Inspector, recorder
  and trace viewer speak the pack's steps: each patch has an anchor in the pinned playwright-core,
  so upgrading Playwright means checking every anchor; bump `patchRevision` whenever a patch changes.
- `internal/filecontent`: reads files by their type (text, CSV, TSV, JSON, XML, HTML, PDF, Word,
  Excel) for the steps that check a file's text or table: files, storage objects, downloads.
- `internal/packbuild`, `internal/gotool`: axx builds itself with the packs a project lists in
  `axx-packs.yaml`, using a Go toolchain it downloads itself.
- `packs/all` and `internal/tools/axxall`: axx with every pack, for generated docs and skills,
  tests and the evals image.
- `internal/compat`: the value semantics steps follow (Jayway JSONPath, Java regex, Java number
  formatting; ADR 0007). The oracle tests depend on these; don't "simplify" them.
- `extensions/wiremock-openapi`, `ide/intellij`: JVM parts (the WireMock extension and the IntelliJ plugin).
- `extensions/mailpit-chaos`: axx's Mailpit image, Mailpit built with `chaos-rules.patch` (rules that refuse
  particular addresses' mail, for the mail pack's refusal steps).
- `ide/vscode`: the VS Code extension (TypeScript). Both editor clients run `axx lsp` (`internal/lsp`).
- `testdata/steps.json`: the frozen catalog of the step text of axx's packs. `testdata/oracles`:
  recorded behavior oracles. **Never edit either by hand**: they define correct behavior. A new
  step joins the catalog with `go test ./internal/engine -run TestStepCatalog -update`, which
  only adds entries.
- `testdata/openapi-agreement`: exchanges that both OpenAPI validators (the REST pack's and the
  WireMock extension's) must report with the same keys; both test suites read it.
- `evals/`: agent evaluations on Harbor (its own Go module; see `evals/README.md`). They run on
  demand only (`.github/workflows/evals.yml`), never on pull requests.

## Rules

- Step expression text is public API. Never change existing step text; add new steps instead.
- Exit codes (`internal/exitcode`) and the `--json` envelope are frozen contracts (ADR 0003).
- Every user-facing error is an `*axxerr.Error` with a stable `AXX-Exxxx` code and a hint.
- Never hand-edit generated files: `go generate ./...` writes the `axx.yaml` schema
  (`internal/config/axx.schema.json`) and the skills (`plugins/axx/skills`), and the docs site's
  build writes the reference (`docs/scripts/gen.mjs`, from `axx docs export`).
- The docs site's root is the latest release's docs, built from its tag; main's are at `/next/`.
  Link pages site-absolute (`/guides/install/`): the build puts `/next/` in front of them.
- `cmd/axx` is the core only. Never import a pack from it: every pack, ours included, gets into
  axx through `axx-packs.yaml`, and pack dependencies stay out of the core binary.
- Keep `CGO_ENABLED=0` builds working: pure-Go dependencies only.
- CI runs the jobs a pull request needs, from the files it changes (`scripts/ci-changes.sh`).
  A new kind of input to a job (a folder its tests read, say) goes there too.
- The `hygiene` check (`scripts/hygiene.sh`) must pass; it blocks references to unrelated
  organizations and internal infrastructure.

## Commands

```sh
mise run test          # go test -race ./...
mise run lint          # golangci-lint run
mise run generate      # go generate ./...: the axx.yaml schema and the skills
mise run hygiene       # denylist + gitleaks
mise run intellij      # a sandbox IntelliJ with the plugin, the Go plugin and axx from this tree
go build -o bin/axx ./cmd/axx                     # the core; prepares a project's packs on first use
go build -o bin/axx-all ./internal/tools/axxall   # every pack built in
```
