# axx

> **Status: beta.** Every `0.x` release is a GitHub pre-release: until `v1.0.0`, flags and
> configuration may change between minor versions. Step text never changes. Nightly builds are
> published from `main`.

**axx** ("axxeptance") is human-readable acceptance testing for the agentic era. Your
acceptance criteria become the tests: scenarios that people can read and coding agents can
write, run black-box against the services you build, fast on a laptop and fast in CI.

```gherkin
Feature: Price quotes

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |

  Scenario: Express delivery abroad
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Express abroad'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | zone         | FR-1 |
      | priceCents   | 1790 |
      | deliveryDays | 2    |
```

```console
$ axx run --compact
PASS 1  380ms
```

[`examples/parcels`](examples/parcels) is a complete suite for a parcel-delivery service: its
API and OpenAPI contract, a mocked dependency, PostgreSQL, MongoDB and Kafka, the logs it writes,
the files it exports and its shop portal in real browsers. Three more examples test services
built on the clouds, with the clouds running locally:

- [`parcel-claims`](examples/parcel-claims): damage claims on AWS.
- [`carrier-billing`](examples/carrier-billing): invoice reconciliation on Google Cloud.
- [`customs-clearance`](examples/customs-clearance): customs declarations on Azure.

## Why axx

- **One small binary.** No JVM, Gradle or test-runner wiring. `axx run` starts your apps,
  waits until they're healthy, runs scenarios in parallel, and cleans up afterwards.
- **Batteries included.** REST with OpenAPI request/response validation, WireMock verification,
  SQL (PostgreSQL first-class; MySQL, SQLite and SQL Server supported), MongoDB, Kafka with
  Avro + Schema Registry, the logs your services write (files, syslog, TCP and HTTP), and the files
  they write: the text of a PDF, Word document or spreadsheet, the rows of a CSV or Excel file.
- **Web apps in real browsers.** Open pages, fill in forms, click buttons, answer dialogs, download
  files and check what the page shows, in Chromium, Firefox or WebKit through Playwright, as a
  desktop or a phone; then check in the same scenario what your service stored and published.
  Watch the browsers as they go, pause a scenario in Playwright's Inspector, and record what you do
  on the page as steps. Compare screenshots, audit accessibility, control what pages fetch, check
  Lighthouse scores and measure JavaScript coverage with the packs that build on it.
- **The clouds your services run on.** S3, SQS, SNS, EventBridge and DynamoDB on AWS; Cloud
  Storage, Pub/Sub, BigQuery and Firestore on Google Cloud; Blob Storage and Service Bus on
  Azure. Every pack talks to the real service through its official SDK, so the same features run
  against a cloud account or a local emulator.
- **Extensible.** Pick the packs a project uses (`axx pack add rest sql`), write your own steps as
  a Go pack that works on the same context as the steps of axx's packs (`axx pack new ./steps`).
- **Built for agents.** `axx steps search`, `axx explain`, `--json` on every command, stable exit
  codes, an MCP server (`axx mcp`) and installable agent skills (`axx skills install`).
- **Test data you can trust.** `axx lint` catches colliding test data before parallel runs
  flake, and `axx fixtures` generates schema-checked fixtures and detects drift.

## Install

```sh
brew install nimbusxr/tap/axx                          # Homebrew (macOS, Linux)
curl -fsSL https://axx.nimbusxr.us/install.sh | sh     # install script (Linux, macOS)
go install github.com/nimbusxr/axx/cmd/axx@latest      # from source
```

On Windows: `irm https://axx.nimbusxr.us/install.ps1 | iex`. There are also the container image
`ghcr.io/nimbusxr/axx`, the `nimbusxr/setup-axx` GitHub Action, and the archives on
[GitHub releases](https://github.com/nimbusxr/axx/releases), with a rolling `nightly` built from
`main`.

## Documentation

<https://axx.nimbusxr.us>, with a Markdown copy of every page for agents and `llms.txt`.

## Contributing

axx is Apache-2.0 licensed and welcomes contributions. See [CONTRIBUTING.md](CONTRIBUTING.md)
(DCO sign-off required) and [AGENTS.md](AGENTS.md) if you work with a coding agent.
Report security issues privately; see [SECURITY.md](SECURITY.md).
