# 0001: Build axx in Go with its own Cucumber executor

- Status: Accepted
- Date: 2026-09-23

## Context

Acceptance tests for services should not require a JVM, build-tool wiring or a test-runner
class in the service's repository, and they should start fast enough to run on every change.
Checks must also be cheap at scale: OpenAPI documents parsed once per run, not per request, and
Kafka topics read without joining a consumer group per assertion.

## Decision

- Build axx as a single Go binary (`CGO_ENABLED=0`, all pure-Go dependencies).
- Build our own executor on the official Cucumber Go libraries (`gherkin`, `messages`,
  `cucumber-expressions`, `tag-expressions`) instead of godog, because godog is regex-only, lacks
  custom parameter types (`{ordinal}`, `{service}`), lacks exclusive-resource scheduling and does
  not emit Cucumber Messages.
- **Step expression text is public API.** It is checked against a frozen catalog of step text
  (`testdata/steps.json`) on every build.
- axx's step packs use the same public pack API as anyone's (ADR 0009).

## Consequences

- Fast startup and execution; one artifact to install.
- Values in steps (JSONPath, regular expressions, numbers) follow the semantics of established
  Java libraries, implemented in `internal/compat` (ADR 0007).
- The WireMock OpenAPI-validation extension and the IntelliJ plugin are JVM components, because
  their hosts only load JVM code.
