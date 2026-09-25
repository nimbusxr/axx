# Contributing to axx

Thanks for helping make acceptance testing fast and pleasant. This guide covers how to set up,
what we expect in a change, and how releases happen.

## Setup

Install [mise](https://mise.jdx.dev), then from the repository root:

```sh
mise install          # Go, golangci-lint, goreleaser, lefthook, gitleaks (+ node/java for docs & JVM parts)
lefthook install      # pre-commit formatting/lint, pre-push short tests
mise run test         # go test -race ./...
go build -o bin/axx ./cmd/axx
```

Integration tests use Docker via testcontainers-go: `go test -tags integration ./...`.

## Making a change

1. **Open an issue first** for anything bigger than a bug fix, so we can agree on the approach.
   Changes to CLI JSON output, exit codes or the `axx.yaml` schema need an ADR
   in `docs/adr/`.
2. **Keep step text stable.** Gherkin step expressions are public API. Adding steps is fine;
   changing or removing one needs a deprecation that ships a replacement first.
3. **Tests are required.** Unit tests for logic, golden tests for rendered output
   (`go test ./... -update` refreshes goldens), and an example scenario for new steps.
4. **Generated files are committed and checked.** Run `mise run generate` after touching the
   step registry, config structs or schemas; CI fails on drift.
5. **Conventional commits.** PRs are squash-merged and the PR title becomes the commit message,
   e.g. `feat(rest): add response header count step` or `fix(kafka): ...`. Allowed types:
   `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`; add `!` for
   breaking changes.

## Developer Certificate of Origin

Every commit must be signed off (`git commit -s`), certifying the
[Developer Certificate of Origin](https://developercertificate.org/). This applies equally to
changes written with AI assistance: a human contributor reviews the change and signs off, and
the PR template asks you to tick "AI-assisted" when it applies.

## Releases

Releases are automated with release-please: merged conventional commits accumulate in a
release PR, and merging that PR tags the version and publishes binaries and images.
While axx is `0.x`, breaking changes bump the minor version.

Nothing is published (releases, images, the Homebrew cask, the IntelliJ plugin, the nightly
build, the docs site, Scorecard results) unless the `AXX_PUBLISH` repository variable is
`true`. Until then, every run of `release.yml` is a dry run, and so is every manual run and
every pull request that touches release files. A dry run reports the release PR
release-please would open, builds every component as a release would, checks it
(`scripts/release-check.sh`) and attaches it to the run. Release secrets live in two environments that only `main` can use: `release-pr` holds the
release app's key for release-please, and `release` holds what publishing needs, with a
maintainer approving each release. To check the CLI release locally:

```sh
goreleaser release --snapshot --clean --skip=sign,sbom
scripts/release-check.sh
```
