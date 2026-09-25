---
title: Architecture decisions
description: The architecture decision records (ADRs) behind Axx's public contracts - building in Go, the CLI contract, releases, the docs platform, the license, value semantics and Go packs.
---

Decisions that shape Axx's public contracts are recorded as ADRs in [`docs/adr`](https://github.com/nimbusxr/axx/tree/main/docs/adr), in [MADR](https://adr.github.io/madr/) format. A change to step text, the pack API, the CLI JSON output or exit codes, the `axx.yaml` schema, or the release policy needs a new ADR.

| ADR | Decision | In short |
| --- | --- | --- |
| [0001](https://github.com/nimbusxr/axx/blob/main/docs/adr/0001-build-in-go.md) | Build Axx in Go with its own Cucumber executor | One static binary; built on the official Cucumber Go libraries; step text is public API. |
| [0002](https://github.com/nimbusxr/axx/blob/main/docs/adr/0002-step-protocol.md) | Language-agnostic step protocol: JSON-RPC 2.0 over stdio | Superseded by 0009. |
| [0003](https://github.com/nimbusxr/axx/blob/main/docs/adr/0003-cli-contract.md) | CLI contract: JSON envelope, exit codes, error codes | Frozen exit codes, one `--json` envelope for every command, stable `AXX-Exxxx` codes, compact output for agents. |
| [0004](https://github.com/nimbusxr/axx/blob/main/docs/adr/0004-versioning-and-release.md) | Versioning and release | `v0.x` pre-releases via release-please and GoReleaser; signed, SBOM'd binaries; the criteria for `v1.0.0`. |
| [0005](https://github.com/nimbusxr/axx/blob/main/docs/adr/0005-docs-platform.md) | Docs platform | This site: Starlight, reference generated from the binary, Markdown twins, `llms.txt`, skills and an MCP server. |
| [0006](https://github.com/nimbusxr/axx/blob/main/docs/adr/0006-license.md) | License | Apache-2.0, contributions signed off with the DCO. |
| [0007](https://github.com/nimbusxr/axx/blob/main/docs/adr/0007-java-compatibility-layer.md) | Value semantics of the established Java libraries, verified by recorded oracles | JSONPath, regular expressions, number formatting and value coercion follow Jayway JsonPath, `java.util.regex` and Jackson exactly. |
| [0008](https://github.com/nimbusxr/axx/blob/main/docs/adr/0008-publish-from-release-workflow.md) | Publish every component from `release.yml` | The CLI, the WireMock image and the IntelliJ plugin publish from one workflow, gated on release-please outputs. |
| [0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md) | Custom steps are Go packs; the `axx` executable loads the packs a project uses | Packs are opt-in per project (`axx-packs.yaml`); custom packs work on the same public contexts as Axx's own. |

Related pages: [Roadmap](/explanations/roadmap/), [Exit and error codes](/references/error-codes/), [JSON output](/references/json-output/).
