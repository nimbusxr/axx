---
title: Roadmap
description: Where Axx is on the way from beta to 1.0, what is in it today, the criteria for v1.0.0, what is intended over the next twelve months, and what Axx will not do.
---

Axx is in **beta**. Versions start at `v0.1.0`, and every `0.x` release is a GitHub pre-release. Until `v1.0.0`, a minor version bump (`0.1` to `0.2`) may contain breaking changes to flags, configuration or what a feature's values mean; features and fixes bump the patch version. Step text is the exception: it is frozen from day one.

## Now

What is in Axx today:

- **The core.** The runner, service lifecycle (`axx up`, `axx down`, `--attach`, `--debug`), debugging step code (`--debug-steps`), pausing web scenarios (`--pause-at`), reporters, `axx init`, `axx doctor`, `axx validate`, `axx explain`, `axx steps`, `axx lint`, `axx fmt` and `axx fixtures`.
- **For agents.** The MCP server (with `steps_try` and the packs' tools), the skills, and `axx host`, which starts services, browsers, devices and apps for an agent whose sandbox keeps it from them ([Run tests from an agent's sandbox](/guides/run-from-a-sandbox/)).
- **Packs for services.** `rest` (with OpenAPI validation), `mock` (WireMock verification, REST, gRPC and GraphQL), `grpc`, `jsonrpc` (with OpenRPC validation), `graphql` (federated graphs and subgraphs), `mcp`, `a2a`, `sql`, `mongo`, `redis`, `kafka`, `amqp`, `mqtt`, `nats`, `websocket`, `sse`, `asyncapi` (AsyncAPI validation of their messages), `logs`, `files`, `mail` and `cli`.
- **Packs for what goes wrong and what services report.** `faults` (connections that are cut off, slow or reset, through Toxiproxy) and `otel` (the traces and logs services report through OpenTelemetry).
- **Packs for web apps.** `web-core` and the packs that build on it: `web-screenshots`, `web-a11y`, `web-network`, `web-lighthouse` and `web-coverage`.
- **Packs for mobile and desktop apps.** `app-core`, with the steps every app takes; `mobile-core`, `mobile-android` and `mobile-ios`; `desktop-core`, `desktop-macos`, `desktop-windows` and `desktop-linux`.
- **Packs for models.** `models-core`, which builds stubs of the models that apps and services run themselves, and the packs for the models a platform gives its apps: `models-ios`, `models-macos`, `models-android`, `models-windows`, `models-linux` and `models-web`.
- **Controls (experimental).** `controls-core`, with `controls-asvs`, `controls-nist` and `controls-oscal` (the report as OSCAL assessment results): scenarios claim the controls they prove with tags, a report shows what covers each required control, and `axx controls check` fails when one is not covered ([Prove security controls](/guides/prove-controls/)).
- **Coverage of the services under test.** `coverage-core`, with `coverage-jvm`, `coverage-go`, `coverage-python` and `coverage-node`, with minimums and `axx coverage check`.
- **Packs for cloud services.** AWS (S3, SQS, SNS, EventBridge, DynamoDB), Google Cloud (Cloud Storage, Pub/Sub, BigQuery, Firestore) and Azure (Blob Storage, Service Bus).
- **Packs of your own.** `axx pack`: choosing packs per project, and custom packs written in Go, loaded the same way as Axx's.
- **Versions and downloads.** A project can name the Axx version it runs on, and `axx upgrade` moves it ([Install](/guides/install/#keep-a-project-on-one-version)). Every download can come from an organization's mirrors ([Use your organization's mirrors](/guides/use-mirrors/)), and `axx docs serve` serves these docs on your machine.
- **Editor support.** The `axx lsp` language server, the IntelliJ plugin and the VS Code extension (feature files, running, debugging and measuring the coverage of scenarios, watching web scenarios' browsers).
- **The OpenAPI-validating WireMock image.**

Since `v0.1.0`, releases are published on GitHub, in the Homebrew tap (`nimbusxr/tap`) and as the container image `ghcr.io/nimbusxr/axx`; the install script and `nimbusxr/setup-axx` install them. A rolling `nightly` pre-release is built from `main` every day.

## Criteria for v1.0.0

From [ADR 0004](https://github.com/nimbusxr/axx/blob/main/docs/adr/0004-versioning-and-release.md), `v1.0.0` ships when these are frozen:

- the `axx.yaml` schema, version 1;
- the CLI contract: the `--json` envelope, exit codes and error codes;
- the [deprecation policy](/explanations/deprecation-policy/);

and when agent evaluations (agents writing and fixing Axx tests from acceptance criteria) meet their success target.

After `v1.0.0`, breaking changes to any of these need a new major version and an ADR.

## The next twelve months

This is what the project has written down as intended. It has set no dates.

1. **Reach `v1.0.0`,** by the criteria above. Until then, a minor version may still change or remove something, and the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md) says so.
2. **Keep the promises that start at `v1.0.0`.** From then on, nothing is removed within a major version, and what is replaced is deprecated first ([Deprecation policy](/explanations/deprecation-policy/)).
3. **Finish what the launcher's plan leaves open.** [ADR 0016](https://github.com/nimbusxr/axx/blob/main/docs/adr/0016-a-macos-launcher-is-the-one-identity-a-person-allows.md) lists two steps that are not done: making the launcher the one identity a hosted macOS runner allows in CI, and updating the desktop decision (ADR 0011) where it describes the checks before a run and CI.

Three decision records are still proposed, not accepted: phones driven through their own drivers ([ADR 0013](https://github.com/nimbusxr/axx/blob/main/docs/adr/0013-phones-through-their-drivers-served-by-device-hosts.md)), the launcher on macOS (ADR 0016) and scrolling on phones ([ADR 0020](https://github.com/nimbusxr/axx/blob/main/docs/adr/0020-a-scroll-is-asked-of-the-view-a-swipe-is-a-finger.md)). Axx already has work from each of them.

During the beta, security fixes go into the latest release only ([SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md)). The [Security](/explanations/security/) page says what you can expect of Axx.

## What Axx will not do

These are decisions, each recorded where the link says.

- **Axx will not run your unit tests, and it is not a load-testing tool.** It tests a running system through its public interfaces ([How Axx works](/explanations/how-axx-works/#what-axx-is-not)).
- **Custom steps are written in Go, and in no other language.** The step protocol for other languages was withdrawn ([ADR 0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md)).
- **No pack is built into the `axx` binary.** The binary is the core, and every pack, Axx's own included, is one a project lists ([ADR 0009](https://github.com/nimbusxr/axx/blob/main/docs/adr/0009-go-packs.md)).
- **Axx's own fixture functions are for hashing and encoding only.** Any other function is one a project adds with a pack ([Fixture factories](/guides/fixture-factories/)).
- **The examples stay out of Axx's Go module.** A project's Axx downloads that module to build its packs, and the examples are a module of their own ([`examples/go.mod`](https://github.com/nimbusxr/axx/blob/main/examples/go.mod)).
- **Axx will not ask you to loosen an agent's sandbox.** A harness's sandbox stays as it is, and Axx works inside it or through `axx host` ([ADR 0018](https://github.com/nimbusxr/axx/blob/main/docs/adr/0018-a-pack-has-a-side-in-the-axx-host.md)).
- **The axx host takes no approval over its port.** A person approves in the host's terminal or in their editor ([ADR 0019](https://github.com/nimbusxr/axx/blob/main/docs/adr/0019-devices-and-apps-start-in-the-axx-host-once-approved.md)).
- **Command hooks run before or after a command, never inside it** ([Run hooks](/guides/run-hooks/), [AGENTS.md](https://github.com/nimbusxr/axx/blob/main/AGENTS.md)).
- **Axx sends no telemetry** ([FAQ](/explanations/faq/#does-axx-collect-telemetry)).

Follow progress in the [GitHub repository](https://github.com/nimbusxr/axx) and the [changelog](https://github.com/nimbusxr/axx/blob/main/CHANGELOG.md).
