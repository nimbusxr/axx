# 0002: Language-agnostic step protocol: JSON-RPC 2.0 over stdio

- Status: Superseded by [0009](0009-go-packs.md)
- Date: 2026-09-23

## Context

Teams must be able to write custom steps in whatever language their service uses. Options
considered: HashiCorp go-plugin/gRPC (forces protobuf tooling), HTTP step servers (ports, auth,
lifecycle), WASM (no practical network/DB access), and Cucumber's deprecated wire protocol
(regex-only, no callbacks, no parameter types).

## Decision

- Plugins are long-lived child processes speaking JSON-RPC 2.0, one JSON message per line on
  stdio (the framing MCP uses). A single process serves many concurrent scenarios by id.
- `initialize` returns a manifest (Cucumber-expression steps with docs/examples/source, parameter
  types, hooks, config schema); the host sends `scenario/start`, `step/run`, `hook/run`,
  `scenario/end`, `$/cancelRequest`, `shutdown`/`exit`.
- Plugins read and write host-owned scenario state via `world/get|set|keys` callbacks with
  namespaced keys (`vars/…`, `rest/<svc>/requests/<n>/response`, …); JSONPath selection runs
  host-side so semantics are identical in every language.
- stdout is reserved for protocol traffic; SDKs redirect language stdout to stderr, which is
  attached to the running step.
- A zero-code tier maps a step expression to an argv command in `axx.yaml`.
- The spec and JSON Schemas live in `protocol/`; `axx plugin test` is the conformance kit.

## Consequences

- Any language with JSON and stdio can extend axx; SDKs (Go, TypeScript, Python, JVM) are
  conveniences, not requirements.
- Manifests are cached so `axx steps`, dry runs, lint and IDE features never spawn plugins.
