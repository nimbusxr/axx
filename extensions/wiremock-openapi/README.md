<!-- SPDX-License-Identifier: Apache-2.0 -->
# axx WireMock OpenAPI validation

A [WireMock](https://wiremock.org) extension that checks the traffic to a mocked dependency
against that dependency's OpenAPI specification. This is the consumer side of the contract: does
your service call its dependency correctly, and does the mock answer the way the real dependency
would? (Your own service's contract, its API as your clients see it, is checked by axx's REST
steps, with settings of its own.)

Every call to a stub with a spec is validated, request and response. The findings are recorded on
WireMock's request journal, where axx reads them: a violation fails the scenario that checks the
call, or the run when no scenario does. Without axx, the extension can instead answer violating
calls with an HTTP 500 that lists the problems.

It ships as a ready-to-run image, `ghcr.io/nimbusxr/axx-wiremock`: WireMock standalone with the
extension on its classpath. Validation uses the Atlassian
[`openapi-request-validator`](https://bitbucket.org/atlassian/swagger-request-validator), which
supports OpenAPI 3.0 and 3.1 and Swagger 2.0, in JSON or YAML.

## Usage with docker-compose

```yaml
services:
  address-service:
    image: ghcr.io/nimbusxr/axx-wiremock:0.1        # pin a version (see Versions below)
    ports:
      - "8081:8080"
    environment:
      OPENAPI_SPEC_SOURCE: /var/openapi/address-service.yaml   # the dependency's contract
      OPENAPI_VALIDATION_MODE: report                          # with axx; see Modes
    volumes:
      - ./wiremock/mappings:/home/wiremock/mappings:ro   # stub mappings
      - ./wiremock/__files:/home/wiremock/__files:ro     # response bodies (optional)
      - ./openapi:/var/openapi:ro                        # specs, anywhere you like
    command: ["--verbose"]                               # any WireMock standalone options
```

WireMock's root directory is `/home/wiremock`, so stubs go in `/home/wiremock/mappings` and
response body files in `/home/wiremock/__files`. Arguments after the image name are passed to
WireMock standalone unchanged. WireMock runs as an unprivileged user (uid 1000).

### Which stubs are validated

`OPENAPI_SPEC_SOURCE` sets the spec every stub is validated against: usually one WireMock per
dependency, so one spec. A stub can name its own spec, or opt out, in its metadata:

```json
{
  "request": { "method": "GET", "urlPathPattern": "/v1/postcodes/[A-Z]{2}/[^/]+" },
  "response": { "status": 200, "jsonBody": { "postcode": "10115", "country": "DE", "deliverable": true } },
  "metadata": { "openApiSpecSource": "/var/openapi/address-service.yaml" }
}
```

`openApiSpecSource` is a path inside the container or a URL. `"openApiValidation": false` turns
validation off for a stub when a default spec is set. Stubs created through the admin API
(`POST /__admin/mappings`) work the same way. A request that matches no stub gets WireMock's
usual 404 and is not validated.

## What happens to a violation

Every validated call gets an `openapi-validation` sub-event on the request journal
(`GET /__admin/requests`), whether or not it broke the contract:

```json
{
  "type": "openapi-validation",
  "data": {
    "format": 1,
    "extension": "0.1.0",
    "spec": "/var/openapi/address-service.yaml",
    "mode": "report",
    "findings": [
      {
        "key": "validation.request.security.missing",
        "level": "ERROR",
        "side": "request",
        "message": "GET on path '/v1/postcodes/DE/10115' requires security parameters. None found."
      }
    ]
  }
}
```

`side` says whose part broke the contract: `request` (your service's call), `response` (the
stub) or `spec` (the spec could not be loaded). `level` is the finding's level after the levels
below were applied: ERROR fails, anything you relaxed it to does not.

axx reads these records (the mock pack, with any WireMock that runs this extension):

- a mock step that checks a call (`the mocked GET request to ... named ... was received by ...`,
  and the steps that refer to its name) fails when that call has an ERROR finding, and logs
  WARN findings;
- ERROR findings on calls no scenario checked fail the run, listed after the scenarios. axx reads
  the journal of every mock the run's scenarios register.

Reading the journal changes nothing in WireMock, so scenarios running in parallel cannot affect
each other.

### Modes

`OPENAPI_VALIDATION_MODE` (or a stub's `"openApiValidationMode"`) decides what the caller gets:

| Mode | A call with an ERROR finding gets |
| --- | --- |
| `fail` (default) | an HTTP 500 with a problem+json body listing the findings |
| `report` | the stub's response, unchanged |

With axx, use `report`: your service gets the answer it would get from the real dependency, and
the test fails with the violation, not with whatever your service does with a 500. Without axx,
`fail` makes a violation visible to anything calling the mock. The 500 body looks like this:

```json
{
  "type": "about:blank",
  "title": "OpenAPI validation failed",
  "status": 500,
  "detail": "POST /users does not match /var/openapi/users.yaml: 1 error",
  "spec": "/var/openapi/users.yaml",
  "findings": [
    { "key": "validation.request.body.schema.required", "level": "ERROR", "side": "request",
      "message": "required property 'name' not found" }
  ]
}
```

## Levels

Each finding has a key, such as `validation.request.body.schema.required` or
`validation.response.status.unknown`, and a level: `ERROR` (or `FAIL`), `WARN`, `INFO` or
`IGNORE`. Every finding is an ERROR until you relax it: setting a key to `WARN` (logged),
`INFO` or `IGNORE` is how you say a deviation is known and must not fail. A key also covers every key below it: `validation.request.body` sets the level of
`validation.request.body.schema.required` unless something more specific is set. The keys are the
ones axx's REST steps use for your own service's contract, but these settings are separate: they
only apply to this dependency.

Levels can be set in three places, from the broadest to the most specific:

| Where | How | For |
| --- | --- | --- |
| the extension | `OPENAPI_VALIDATION_LEVELS=validation.request.parameter.header=WARN,validation.response.body.schema.additionalProperties=IGNORE` | every stub of this mock |
| a stub | `"metadata": {"openApiValidationLevels": {"validation.response.body": "WARN"}}` | the calls that stub answers: the place for a stub that is off-contract on purpose |
| an axx scenario | `Given the OpenAPI validation levels for the mocked addresses service are:` | the calls that scenario's mock steps check |

A more specific place wins over a broader one as a whole: a key a stub sets, however broad, is not
overridden by a more specific key in the extension's settings. Without any setting a finding is an
ERROR, except `validation.request.parameter.query.unexpected` (a query parameter the spec does not
declare), which is IGNORE.

## Configuration

| Environment variable | Effect |
| --- | --- |
| `OPENAPI_SPEC_SOURCE` | The spec (path or URL) for stubs that do not name one. |
| `OPENAPI_VALIDATION_MODE` | `fail` (default) or `report`. |
| `OPENAPI_VALIDATION_LEVELS` | Default levels, `key=LEVEL` pairs separated by commas. |
| `OPENAPI_SPEC_AUTH_HEADER` | A header (`Name: value`) sent when fetching specs from URLs, e.g. `Authorization: Bearer ...`. |
| `OPENAPI_VALIDATION_TRUST_ALL_CERTS=true` | Skip TLS certificate and host name checks when fetching specs over HTTPS (self-signed certificates). Only the spec downloads are affected. |
| `JAVA_OPTS` | Extra JVM options, for example `-Xmx256m`. |
| `/var/wiremock/extensions/*.jar` | Mount more WireMock extension jars here. They are added to the classpath. The bundled validator lives elsewhere, so mounting this directory does not hide it. |

A spec is parsed once and cached. A spec file that changes is loaded again on the next call
(files it references are not watched), and a spec that fails to load is retried after 5 seconds.
Specs from URLs stay cached until `POST /__admin/openapi-validation/reset`.

The image's healthcheck calls `GET /__admin/health` on port 8080. If you move WireMock to another
port with `--port`, override the healthcheck in your compose file.

### Admin endpoints

| Endpoint | Returns |
| --- | --- |
| `GET /__admin/openapi-validation` | the extension's version, findings format, mode, levels and default spec |
| `POST /__admin/openapi-validation/reset` | forgets every cached spec (`{"cleared": n}`) |

### Registration

The extension registers itself through WireMock's `ServiceLoader` scanning
(`META-INF/services/com.github.tomakehurst.wiremock.extension.Extension`). Scanning is on by
default in WireMock standalone, so you don't need an `--extensions` flag.

If you start WireMock with `--disable-extensions-scanning`, name the extension explicitly:
`--extensions us.nimbusxr.axx.wiremock.openapi.OpenApiValidatorExtension`.

### Using the jar without the image

The extension needs **Java 21** and WireMock **3.13.x standalone**. The official
`wiremock/wiremock` image runs Java 17, so it cannot load the extension. Every release attaches the
fat jar (`axx-wiremock-openapi-<version>-all.jar`) to its GitHub release. Put it on the classpath
next to `wiremock-standalone.jar`:

```sh
java -cp 'wiremock-standalone.jar:axx-wiremock-openapi-0.1.0-all.jar' wiremock.Run --verbose
```

With the Java API, register it with `extensions(OpenApiValidatorExtension.class)` or
`extensionScanningEnabled(true)`.

### OpenAPI 3.1 and `nullable`

OpenAPI 3.1 removed the 3.0 `nullable` keyword. A 3.1 spec must write nullable fields as
`"type": ["string", "null"]`. A 3.1 spec that still says `nullable: true` rejects `null` values.

## Versions

The extension is versioned on its own (SemVer), next to axx: merging its release-please PR tags
`wiremock-openapi-v<version>` and publishes:

| Image tag | Moves? | Use it for |
| --- | --- | --- |
| `<version>`, e.g. `0.1.0` | never | reproducible setups; bump it on purpose |
| `<major>.<minor>`, e.g. `0.1` | with every patch release of that line | compose files that should get fixes |
| `latest` | with every release | trying things out, never CI |
| `edge` | with every change on `main` | testing unreleased changes |

Each extension version pins one WireMock version (`WIREMOCK_VERSION` in the `Dockerfile`); a
WireMock upgrade is an extension release. Until `1.0.0` a minor version may break things, as in
axx itself.

**axx and the extension.** axx reads the extension's records through two things only: the
`openapi-validation` sub-event and its `format` number. New fields in a record keep the format; a
change axx must understand gets a new format number and a new minor (before 1.0) or major
version. axx reads one format and says so when a mock records another, with the versions to line
up. `GET /__admin/openapi-validation` shows the version and format of a running mock.

| axx | axx-wiremock | Findings format |
| --- | --- | --- |
| 0.1.x | 0.1.x | 1 |

## Building

Requires JDK 21 (`mise install` at the repository root provides it). This is a standalone Gradle
build: run it from this directory.

```sh
./gradlew test        # unit and integration tests (against the shaded jar)
./gradlew shadowJar   # build/libs/axx-wiremock-openapi-<version>-all.jar
docker build -t axx-wiremock:dev .
./smoke-test.sh axx-wiremock:dev
```

The WireMock version is pinned in `build.gradle.kts` and in the `Dockerfile`
(`WIREMOCK_VERSION`, plus `WIREMOCK_SHA256` for the jar's checksum). Update them together.

WireMock standalone bundles a relocated copy of the networknt JSON schema validator. It leaves
that copy's message bundle (`jsv-messages*.properties`) at the classpath root, where it would
override the one the validator needs and garble messages into `required property '{1}' not
found`. The fat jar therefore renames its copy of the bundle, and the tests run against the fat
jar with WireMock ahead of it on the classpath, as in the image.
