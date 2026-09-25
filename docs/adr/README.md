# Architecture Decision Records

Decisions that shape axx's public contracts, in [MADR](https://adr.github.io/madr/) format.
Add one (next number, `NNNN-kebab-title.md`) for any change to step text,
CLI JSON output or exit codes, the `axx.yaml` schema, or release/versioning policy.

| ADR | Title | Status |
|-----|-------|--------|
| [0001](0001-build-in-go.md) | Build axx in Go with its own Cucumber executor | Accepted |
| [0002](0002-step-protocol.md) | Language-agnostic step protocol: JSON-RPC 2.0 over stdio | Superseded by 0009 |
| [0003](0003-cli-contract.md) | CLI contract: JSON envelope, exit codes, error codes | Accepted |
| [0004](0004-versioning-and-release.md) | Versioning and release: v0.x pre-releases via release-please + GoReleaser | Accepted |
| [0005](0005-docs-platform.md) | Docs platform: Starlight, generated reference, agent-first outputs | Accepted |
| [0006](0006-license.md) | License Apache-2.0 with DCO | Accepted |
| [0007](0007-java-compatibility-layer.md) | Value semantics of the established Java libraries, verified by recorded oracles | Accepted |
| [0008](0008-publish-from-release-workflow.md) | Publish every component from release.yml, gated on release-please outputs | Accepted |
| [0009](0009-go-packs.md) | Custom steps are Go packs; the axx executable loads the packs a project uses | Accepted |
