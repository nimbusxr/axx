---
title: FAQ
description: Common questions about Axx - the name, how it relates to Cucumber, what it needs, and how it fits with other testing tools.
---

### Why "axxeptance"?

It is *acceptance* testing, with a name of its own. Axx is the short form, and the command is `axx`.

### Do I need Java, Node or Python?

No. Axx is a single static binary, and your service can be written in anything. What Axx needs, it downloads itself and keeps in its cache: the Go toolchain it uses to build itself with a project's [packs](/guides/use-packs/), custom ones included, and the Node.js and browsers the web packs drive. Docker is only needed if your apps start with Docker Compose.

### Is Axx Cucumber?

Axx runs Gherkin, the language Cucumber defined, with its own executor built on the official Cucumber libraries for Go (the Gherkin parser, Cucumber Expressions, tag expressions and Cucumber Messages). Feature files are standard Gherkin, and reports use Cucumber formats. You do not write step definitions for what Axx's packs cover: their steps come with Axx.

### How is this different from Postman, Karate or REST Assured?

Axx tests a whole service from the outside, not only its HTTP API: it seeds databases, publishes and consumes events, verifies calls to mocked dependencies, and validates everything against your OpenAPI document. It also manages the service's lifecycle (start, wait, stop, clean up) and runs scenarios in parallel. Scenarios are plain Gherkin, readable by the people who wrote the acceptance criteria.

### Can I test services that are not HTTP?

Yes. Axx's packs cover SQL databases, MongoDB, Kafka, the logs and files your services write, and cloud services on AWS, Google Cloud and Azure. Anything else can be reached with a [custom step](/guides/write-custom-steps/), written as a Go pack.

### Does Axx run my unit tests?

No. Keep unit and integration tests in your language's test framework. Axx tests the running system through its public interfaces.

### Can Axx test a deployed environment?

Yes. Write the part of the service URLs that differs as a property (`http://${sys:parcels.host}:8400`), set it for the environment in a profile (say `--profile staging`), and run with `--no-start` so Axx does not try to start apps. Keep data isolation in mind: the environment is shared with everyone else.

### Why do my scenarios pass alone and fail together?

They share data. Scenarios run in parallel against shared infrastructure, so each one needs its own ids, names and keys. See [Scenario isolation](/explanations/scenario-isolation/) and [`axx lint`](/guides/isolate-test-data/).

### Does Axx collect telemetry?

No. Axx sends no telemetry and does not check for updates. The only downloads it makes are those it needs, once, into its cache: the Go toolchain and the packs' Go modules when it builds itself with a project's packs, and the Node.js, Playwright, browsers, axe-core and Lighthouse the web packs use. Otherwise it talks only to the services, databases, brokers and clouds your scenarios name.

### What license is Axx under?

Apache-2.0. Contributions are welcome with a DCO sign-off (`git commit -s`); see [CONTRIBUTING.md](https://github.com/nimbusxr/axx/blob/main/CONTRIBUTING.md).

### Where do I report a bug or a security issue?

Bugs and questions: [GitHub issues](https://github.com/nimbusxr/axx/issues). Security issues privately, as described in [SECURITY.md](https://github.com/nimbusxr/axx/blob/main/SECURITY.md).
