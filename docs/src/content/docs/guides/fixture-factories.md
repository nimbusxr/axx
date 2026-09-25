---
title: Fixture factories
description: Generate schema-valid seeds, event payloads and mock bodies from a compact factory spec with axx fixtures, and catch drift when a schema changes.
---

Suites accumulate dozens of fixture files that share one schema: Kafka payloads for one Avro record, seed datasets for one set of tables, mock bodies for one API. Most of each file is boilerplate, and the day the schema gains a required field, every file needs the same edit. A **factory** keeps the shared shape once and each fixture as only its differences. `axx fixtures` expands them into ordinary files.

## The files

Everything lives next to the fixtures it produces:

```text
kafka/
  depot-scans.factory.yaml             # family, schema, identities
  depot-scans.prototype.yaml           # the shared shape
  scan-delivered.fixture.yaml          # one fixture: only its differences
  scan-out-for-delivery.fixture.yaml
  scan-delivered.json                  # generated
  scan-out-for-delivery.json           # generated
```

```yaml title="kafka/depot-scans.factory.yaml"
# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx-factory.schema.json
factory:
  family: avro
  schema: ../schemas/depot-scan.avsc   # relative to this file
identity:
  - path: scanId                       # unique across every fixture
```

```yaml title="kafka/depot-scans.prototype.yaml"
data:
  parcelRef: PX-EXAMPLE-1
  location: Leipzig
  scannedAt: "2026-05-06T10:15:00Z"
```

```yaml title="kafka/scan-delivered.fixture.yaml"
data:
  scanId: SC-FIXTURE-DELIVERED
  status: DELIVERED
```

The fixture's file name is its name, and `scan-delivered.json` is what features reference, exactly as if you had written it by hand:

```json title="kafka/scan-delivered.json (generated)"
{
  "scanId": "SC-FIXTURE-DELIVERED",
  "parcelRef": "PX-EXAMPLE-1",
  "status": "DELIVERED",
  "location": "Leipzig",
  "scannedAt": "2026-05-06T10:15:00Z"
}
```

## Generate and check

```sh
axx fixtures generate   # write the fixture files, the manifest and lint rules
axx fixtures check      # CI: regenerate in memory and compare, without writing
```

- **Generation is a development-time step.** Nothing is generated during `axx run`; features cannot tell a generated fixture from a hand-written one.
- **Output is deterministic.** The same spec always produces the same bytes, and every output is validated against the schema (the Avro schema here, so a `status` that is not one of its symbols fails) before it is written.
- **The tool only touches files it owns.** `axx-fixtures.manifest.yaml` records them with their checksums. A managed file that was edited by hand is refused, never overwritten: move the change into the spec.
- **Identities become lint rules.** Each `identity:` entry produces a rule in `axx-lint.generated.yaml`, which `lint.include` pulls into [`axx lint`](/guides/isolate-test-data/). A fixture that omits an identity value gets one derived from its name, so it is unique by construction.

When the schema gains a required field, generation fails and names every fixture that lacks it. Add it once to the prototype (or a `defaults:` entry) and regenerate.

## Families

| Family | Produces | Schema |
| --- | --- | --- |
| `avro` | Kafka payload JSON | an `.avsc` file |
| `json` | any JSON: mock bodies, request payloads | JSON Schema, or an OpenAPI component (`api.yaml#/components/schemas/Name`) |
| `yaml` | record-shaped YAML | the same as `json` |
| `xml` | XML documents | an XSD |
| `protobuf` | canonical proto-JSON | a descriptor set (`orders.desc#pkg.Message`) |
| `dataset` | SQL seeds: YAML datasets, flat XML or CSV directories | optional SQL DDL (a file or a directory of migrations) |

For `dataset`, the prototype is a row template per table, and identities are `table.column` paths enforced per row. `options: { format: xml }` or `{ format: csv }` in the factory writes flat XML or a CSV directory instead of YAML. The parcels example generates its rejected manifest lines as flat XML:

```yaml title="seeds/manifests/xml/manifests-xml.factory.yaml"
factory:
  family: dataset
  options: { format: xml }
  schema: ../../../../infra/postgres/init/01-schema.sql
```

## Project settings

```yaml title="axx.yaml"
fixtures:
  sources: ["../infra/wiremock"]      # extra roots, e.g. mock bodies served by a container
  output: { ignored: true }           # generated files are gitignored, not committed
  conformance:                        # lint hand-written files against a schema too
    - name: seeds match the database schema
      filePatterns: ["seeds/*.yaml"]
      schemaType: dataset
      schemaRef: ../infra/postgres/init/01-schema.sql
```

With `output: { ignored: true }`, generated files are derived on demand instead of committed: Axx maintains an exact `.gitignore` in each output directory, and a fresh clone runs `axx fixtures generate` once before `axx run`. The specs, the manifest and the generated lint rules stay committed; they are what reviewers read.

`conformance` rules check files that no factory owns, so hand-written seeds are held to the same schema.

## Adopt existing files

You do not need to write specs for fixtures you already have. Adoption reads existing files, writes the most common value of each field into the prototype, keeps each file's differences in its own `*.fixture.yaml`, and proposes identity candidates for you to review. It only writes anything if regenerating from the new spec reproduces the originals exactly.
