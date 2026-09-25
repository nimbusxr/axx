---
title: Roadmap
description: Where Axx is on the way from beta to 1.0, what is in it today, and the criteria for v1.0.0.
---

Axx is in **beta**. Versions start at `v0.1.0`, and every `0.x` release is a GitHub pre-release. Until `v1.0.0`, a minor version bump (`0.1` to `0.2`) may contain breaking changes to flags or configuration; features and fixes bump the patch version. Step text is the exception: it is frozen from day one, so feature files keep working across every release.

## Now

- The core runner, app lifecycle (`axx up`, `axx down`, `--attach`, `--debug`), reporters, `axx init`, `axx doctor`, `axx validate`, `axx explain`, `axx steps`, the MCP server and the skills.
- Packs: `rest` (with OpenAPI validation), `mock` (WireMock verification), `sql`, `mongo` and `kafka`.
- `axx lint` and `axx fixtures`.
- `axx pack`: choosing packs per project, and custom packs written in Go, loaded the same way as Axx's.
- Editor support: the `axx lsp` language server, the IntelliJ plugin (feature files and debugging) and the VS Code extension.
- The OpenAPI-validating WireMock image.

Release channels (Homebrew, the install script, the container image and `nimbusxr/setup-axx`) go live with `v0.1.0`. A rolling `nightly` pre-release is built from `main` until then.

## Criteria for v1.0.0

From [ADR 0004](https://github.com/nimbusxr/axx/blob/main/docs/adr/0004-versioning-and-release.md), `v1.0.0` ships when these are frozen:

- the `axx.yaml` schema, version 1;
- the CLI contract: the `--json` envelope, exit codes and error codes;
- a deprecation policy;

and when agent evaluations (agents writing and fixing Axx tests from acceptance criteria) meet their success target.

After `v1.0.0`, breaking changes to any of these need a new major version and an ADR.

Follow progress in the [GitHub repository](https://github.com/nimbusxr/axx) and the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md).
