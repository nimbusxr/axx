---
title: Roadmap
description: Where Axx is on the way from beta to 1.0, what is in it today, and the criteria for v1.0.0.
---

Axx is in **beta**. Versions start at `v0.1.0`, and every `0.x` release is a GitHub pre-release. Until `v1.0.0`, a minor version bump (`0.1` to `0.2`) may contain breaking changes to flags or configuration; features and fixes bump the patch version. Step text is the exception: it is frozen from day one, so feature files keep working across every release.

## Now

- The core runner, service lifecycle (`axx up`, `axx down`, `--attach`, `--debug`), debugging step code (`--debug-steps`), pausing web scenarios (`--pause-at`), reporters, `axx init`, `axx doctor`, `axx validate`, `axx explain`, `axx steps`, the MCP server (with `steps_try` and the packs' tools) and the skills.
- Packs: `rest` (with OpenAPI validation), `mock` (WireMock verification, REST, gRPC and GraphQL), `grpc`, `jsonrpc` (with OpenRPC validation), `graphql` (federated graphs and subgraphs), `sql`, `mongo`, `redis`, `kafka`, `amqp`, `mqtt`, `nats`, `websocket`, `sse`, `asyncapi` (AsyncAPI validation of their messages), `logs`, `files`, `mail` and `cli`; `web-core` and the web packs that build on it (`web-screenshots`, `web-a11y`, `web-network`, `web-lighthouse`, `web-coverage`); the cloud packs for AWS (S3, SQS, SNS, EventBridge, DynamoDB), Google Cloud (Cloud Storage, Pub/Sub, BigQuery, Firestore) and Azure (Blob Storage, Service Bus).
- `axx lint` and `axx fixtures`.
- `axx pack`: choosing packs per project, and custom packs written in Go, loaded the same way as Axx's.
- Editor support: the `axx lsp` language server, the IntelliJ plugin and the VS Code extension (feature files, running and debugging scenarios, watching web scenarios' browsers).
- The OpenAPI-validating WireMock image.

Since `v0.1.0`, releases are published on GitHub, in the Homebrew tap (`nimbusxr/tap`) and as the container image `ghcr.io/nimbusxr/axx`; the install script and `nimbusxr/setup-axx` install them. A rolling `nightly` pre-release is built from `main` every day.

## Criteria for v1.0.0

From [ADR 0004](https://github.com/nimbusxr/axx/blob/main/docs/adr/0004-versioning-and-release.md), `v1.0.0` ships when these are frozen:

- the `axx.yaml` schema, version 1;
- the CLI contract: the `--json` envelope, exit codes and error codes;
- a deprecation policy;

and when agent evaluations (agents writing and fixing Axx tests from acceptance criteria) meet their success target.

After `v1.0.0`, breaking changes to any of these need a new major version and an ADR.

Follow progress in the [GitHub repository](https://github.com/nimbusxr/axx) and the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md).
