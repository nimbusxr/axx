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

## Identities

An `identity:` entry names a field that tells fixtures apart, like `scanId` above. A fixture that gives it a value keeps that value; one that leaves it out gets one derived from its name (`prefix:` goes in front, and `format: uuid-name-based` makes it a UUID). Generation fails when two fixtures, of any factories, have the same value in identity fields of the same name, and names both fixtures.

- **Fields of different names never collide.** A shop's `key` and a pickup's `id` can both be `alder-stationery`. So can two identity fields of one fixture: each identity is checked on its own.
- **A namespace groups identities by what they identify.** Give identities a `namespace:` when their field names say too little. Different namespaces keep fields of one name apart: a depot's `id` and a shop's `id` can both be `leipzig`. One namespace compares fields of different names: a SQL seed's `shops.shop_id` and a registration payload's `shopId` that both create shops.

```yaml title="depots/depots.factory.yaml"
identity:
  - path: id
    namespace: depots     # a depot's id may equal a shop's
```

Before namespaces, identity values were unique across every identity field of every factory. To keep that, give every identity the same namespace.

## Reference other fixtures

A value can be another fixture, or part of one, with `$ref`: the path of its `*.fixture.yaml` (or `*.factory.yaml`) file, relative to the file that refers to it (a leading `/` starts at the directory of `axx.yaml`, or `fixtures.baseDir`), then `#` and a JSON Pointer into it.

```yaml title="seeds/handover.fixture.yaml"
data:
  parcel:
    $ref: ../parcels/express-berlin.fixture.yaml        # the whole fixture
  parcelId:
    $ref: ../parcels/express-berlin.fixture.yaml#/id    # one value of it
```

A reference sees the fixture as it is generated: its prototype, its defaults, and the identities derived for it. Keys next to `$ref` override what it brings. References that loop fail and name the loop; a path that names no fixture says what it was resolved to, and the fixtures it most likely meant.

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

## Where a value comes from

A generated value comes from the fixture, a prototype overlay, the prototype, a `defaults:` entry or the schema's default, and it may get there through a `$ref`, an expression or an identity. `axx fixtures explain` names the source to edit, and the place in it:

```sh
axx fixtures explain kafka/scan-delivered.json location
```

```text
location = "Leipzig"
  in kafka/scan-delivered.json, fixture scan-delivered of kafka/depot-scans.factory.yaml
  set by the prototype at data.location in kafka/depot-scans.prototype.yaml
```

The path is dotted, with indices (`recipient.postcode`, `items[0].sku`); a dataset's starts with its table (`parcels.manifest_lines[0].weight_grams`). A fixture's `*.fixture.yaml` works in place of the file it generates. With `--json`, the origins come as a list, outermost first: a `$ref`, then the source of what it points at.

A payload the cloud wraps and encodes on its way to your service, such as a Pub/Sub push envelope with the message base64-encoded in it, is not a fixture to build: publish the message itself, and the emulator's push subscription delivers the envelope ([Send a service messages](/guides/test-cloud-services/#send-a-service-messages)).

## Families

| Family | Produces | Schema |
| --- | --- | --- |
| `avro` | Kafka payload JSON | an `.avsc` file |
| `json` | any JSON: mock bodies, request payloads, messages | JSON Schema, an OpenAPI component (`api.yaml#/components/schemas/Name`), or an AsyncAPI message's payload (`asyncapi.yaml#/components/messages/Name`) |
| `yaml` | record-shaped YAML | the same as `json` |
| `xml` | XML documents | an XSD |
| `protobuf` | canonical proto-JSON | a `.proto` file or a descriptor set, with the message (`<file>.proto#pkg.Message`, `<set>.desc#pkg.Message`) |
| `dataset` | SQL seeds: YAML datasets, flat XML or CSV directories | optional SQL DDL (a file or a directory of migrations) |

A JSON Schema can reference definitions in other local files, by a path relative to the schema holding the `$ref`, with or without a fragment: `"$ref": "address.schema.json"` or `"$ref": "common/money.schema.json#/$defs/Money"`. A failure a referenced file's rule causes names that file. Remote schemas are not fetched: save them next to yours.

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

In CI, check before you generate:

```sh
axx fixtures check      # the sources, the committed manifest and any generated files agree
axx fixtures generate   # write the ignored outputs this checkout lacks
axx run
```

A fresh clone checks clean: the ignored outputs it lacks pass when the committed manifest records what the sources produce. Changed sources whose manifest was not generated again fail, and so does a generated file edited by hand. The other order would hide a stale manifest, because generate brings it up to date.

`conformance` rules check files that no factory owns, so hand-written seeds are held to the same schema.

## Adopt existing files

You do not need to write specs for fixtures you already have. Adoption reads existing files, puts the values they all share into the prototype, keeps each file's differences in its own `*.fixture.yaml`, and proposes identity candidates for you to review. It only writes anything if regenerating from the new spec reproduces the originals exactly.

Try it first with `--dry-run`, which does everything but write:

```sh
axx fixtures adopt --family json --schema openapi/parcels.yaml#/components/schemas/Parcel \
  --files 'mocks/parcels/*.json' --factory parcel-bodies --dry-run
```

```text
  decoded 3/3 files (family: json)
  prototype: 4 field(s) (the values every adopted file shares)
  identity candidates, not declared: id (distinct in all 3), trackingNumber (distinct in all 3); declare the ones that identify a fixture with --identity <path>
  would write mocks/parcels/parcel-bodies.factory.yaml + 3 *.fixture.yaml + parcel-bodies.prototype.yaml
  semantic round-trip: 3/3 deep-equal. 0 file(s) will be reformatted by `axx fixtures generate`, 0 semantic changes.
  next: axx fixtures generate
```

- **The prototype takes only what every file shares.** A value two files of three happen to have stays in those two files, so no fixture's customer or tenant becomes every new fixture's default. `--prototype common` takes each field's most common value instead.
- **Identities are yours to declare.** A candidate is a string field whose value is distinct in every file, which is what an identity looks like, but not proof of one: an email or a domain can be distinct in three examples by chance. Declare the ones that identify a fixture with `--identity` (repeatable); the factory file lists the others as comments.
- **A refusal says what would change.** When regenerating would not reproduce the originals, adoption lists the paths that would differ and writes nothing. Seed layouts whose maps are keyed by document ID (`orgs: {alpha: {...}}`) often do this: adopt the records as a factory of their own, then reference them from the layout with `$ref`.

```sh
axx fixtures adopt --family json --schema openapi/parcels.yaml#/components/schemas/Parcel \
  --files 'mocks/parcels/*.json' --factory parcel-bodies --identity id
axx fixtures adopt --into parcel-bodies --files 'mocks/returns/*.json'   # more files into the same factory
axx fixtures generate
```

`--into` adds files to an existing factory, with its prototype and identities.
