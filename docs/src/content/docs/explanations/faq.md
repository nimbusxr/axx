---
title: FAQ
description: Common questions about Axx - the name, how it relates to Cucumber, what it needs, and how it fits with other testing tools.
---

### Why "axxeptance"?

It is *acceptance* testing, with a name of its own. Axx is the short form, and the command is `axx`.

### Do I need Java, Node or Python?

No. Axx is a single static binary. Your service can be written in anything, and Axx itself needs nothing installed. Docker is only needed if your apps start with Docker Compose, and Go only if your project adds [custom packs](/guides/use-packs/).

### Is Axx Cucumber?

Axx runs Gherkin, the language Cucumber defined, with its own executor built on the official Cucumber libraries for Go (the Gherkin parser, Cucumber Expressions, tag expressions and Cucumber Messages). Feature files are standard Gherkin, and reports use Cucumber formats. You do not write step definitions for the built-in capabilities: they ship with Axx.

### How is this different from Postman, Karate or REST Assured?

Axx tests a whole service from the outside, not only its HTTP API: it seeds databases, publishes and consumes events, verifies calls to mocked dependencies, and validates everything against your OpenAPI document. It also manages the service's lifecycle (start, wait, stop, clean up) and runs scenarios in parallel. Scenarios are plain Gherkin, readable by the people who wrote the acceptance criteria.

### Can I test services that are not HTTP?

Yes. SQL databases, MongoDB and Kafka are built in, and anything else can be reached with a [custom step](/guides/write-custom-steps/), written as a Go pack.

### Does Axx run my unit tests?

No. Keep unit and integration tests in your language's test framework. Axx tests the running system through its public interfaces.

### Can Axx test a deployed environment?

Yes. Point the service URLs at the environment (with a profile, say `--profile staging`) and run with `--no-start` so Axx does not try to start apps. Keep data isolation in mind: the environment is shared with everyone else.

### Why do my scenarios pass alone and fail together?

They share data. Scenarios run in parallel against shared infrastructure, so each one needs its own ids, names and keys. See [Scenario isolation](/explanations/scenario-isolation/) and [`axx lint`](/guides/isolate-test-data/).

### Does Axx collect telemetry?

No. Axx makes no network calls of its own; it only talks to the services, databases and brokers your scenarios name.

<!-- TODO(verify): confirm there is still no telemetry or update check before the first release. -->

### What license is Axx under?

Apache-2.0. Contributions are welcome with a DCO sign-off (`git commit -s`); see [CONTRIBUTING.md](https://github.com/nimbusxr/axx/blob/main/CONTRIBUTING.md).

### Where do I report a bug or a security issue?

Bugs and questions: [GitHub issues](https://github.com/nimbusxr/axx/issues). Security issues privately, as described in [SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md).
