# 0004: Versioning and release: v0.x pre-releases via release-please + GoReleaser

- Status: Accepted
- Date: 2026-09-23

## Decision

- Versions start fresh at `v0.1.0` (the Go module path is new, so no `/vN` suffix). Every `0.x`
  release is marked as a GitHub pre-release until `v1.0.0`.
- release-please (manifest mode) maintains a release PR from conventional commits; merging it
  creates the tag and release. Pre-1.0, breaking changes bump the minor version and features
  bump the patch version.
- GoReleaser runs in the same workflow when a release is created, attaching signed (cosign
  keyless), SBOM'd, provenance-attested binaries for linux/darwin/windows × amd64/arm64, GHCR
  images and the Homebrew cask `nimbusxr/tap/axx`.
- Because `releases/latest` skips pre-releases, install scripts and `setup-axx` resolve
  versions via the releases list API.
- The JVM components (the WireMock extension and the IntelliJ plugin) are separate
  release-please components with their own tags.
- A rolling `nightly` pre-release is built from `main`.

## GA criteria for v1.0.0

The `axx.yaml` v1 schema, CLI JSON/exit-code contract and deprecation
policy are frozen, and agent-eval success meets its target.
