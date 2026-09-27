---
name: axx-test-data
description: Keep the test data of Axx acceptance tests (the human-readable acceptance testing framework) - SQL and MongoDB seeds, Kafka events, request payloads, mock bodies - in fixture factories that check it against its schema and keep its ids unique. Covers axx fixtures (generate, check, adopt, clean), factories, prototypes, fixtures, $ref, identities, generated lint rules, axx lint and CI. Use when several scenarios need payloads, seeds or mock bodies of one shape, or hand-written ones repeat.
license: Apache-2.0
---

# Test data with Axx

Scenarios read their data from files: `Given a seeds/portal-find.yaml db seed`, `the depot-scans kafka event payload is a kafka/scan-delivered.json resource`. Those files can be written by hand, and one-off files should be. **Fixture factories are optional**, but use them wherever data repeats: when several files share one schema and differ in a few fields.

A factory keeps the shared shape once and each fixture as only its differences. `axx fixtures generate` expands them into ordinary files, checked against their schema, with unique ids. Features cannot tell a generated file from a hand-written one, and nothing is generated during `axx run`.

## When to use a factory

- **Three or more files of one shape**: seeds of one table, events of one Avro record, bodies of one API response. `axx lint` prints a `hint:` when scenarios read several hand-written files of one shape that no factory generates.
- **A new scenario needs data like an existing file's.** Add a fixture to its factory instead of copying the file.
- **Hand-written files already repeat.** `axx fixtures adopt` turns them into a factory without changing a value (see "Adopt existing files").
- **Not** for a single file, or for files that share nothing.

## The routine

1. **Look for a factory first.** Factories are `*.factory.yaml` files, next to the files they generate. `axx-fixtures.manifest.yaml` lists every file a factory generates.
2. **Add a fixture:** a `<name>.fixture.yaml` next to the factory, with only what differs from the prototype. Its file name is the fixture's name and the name of the file it generates.
3. **Give it unique data.** Scenarios run in parallel against shared databases: every id, reference and key must be the fixture's own. Identities do this for you (see "Identities").
4. **Generate:** `axx fixtures generate`. It fails, and names the fixture and the field, when a value breaks the schema or an identity collides.
5. **Use the generated file** in the step, exactly as a hand-written one.
6. **Check:** `axx fixtures check` (nothing drifted), then `axx lint` (no id collides with another file).

Never edit a generated file: generate refuses to overwrite a hand-edited one (`AXX-E0903`). Change the fixture, the prototype or the factory, then generate again. To remove a fixture, delete its `*.fixture.yaml` and the file it generated, then generate: the old file is no longer the factory's.

## The files

Everything lives next to what it produces. SQL seeds for the parcels table, say:

```text
seeds/portal/
  portal-parcels.factory.yaml     # family, schema, identities
  portal-parcels.prototype.yaml   # the shape every fixture starts from
  portal-find.fixture.yaml        # one fixture: only its differences
  portal-open.fixture.yaml
  portal-find.yaml                # generated
  portal-open.yaml                # generated
```

```yaml title="seeds/portal/portal-parcels.factory.yaml"
# yaml-language-server: $schema=https://axx.nimbusxr.us/schemas/v0/axx-factory.schema.json
factory:
  family: dataset
  schema: ../../../infra/postgres/init/01-schema.sql   # relative to this file
identity:
  - path: parcels.parcels.reference
    prefix: PX-WEB-
```

```yaml title="seeds/portal/portal-parcels.prototype.yaml"
data:
  parcels.parcels:              # datasets: one row template per table
    sender: lark-and-loom
    status: REGISTERED
    weight_grams: 1200
    recipient: '{"name": "Felix Hartmann", "city": "Stuttgart", "postcode": "70173", "country": "DE"}'
```

```yaml title="seeds/portal/portal-open.fixture.yaml"
data:
  parcels.parcels:
    - reference: PX-WEB-5141
      status: IN_TRANSIT
      service_level: EXPRESS
```

`portal-open.yaml` is generated with the prototype's values under the fixture's, and the feature reads it with `Given a seeds/portal/portal-open.yaml db seed`. Maps merge; lists and scalars replace. An empty `*.fixture.yaml` is a fixture that is exactly the prototype.

Each file kind has a JSON Schema: `axx schema --kind factory`, `--kind fixture` and `--kind prototype` print them, and `references/spec-files.md` has all three. Put the `# yaml-language-server: $schema=...` line at the top for completion in editors.

### The factory

- `factory.family` and `factory.schema`: what it generates and what checks it (table below). The schema path is relative to the factory file.
- `factory.output.dir`: write every output there (relative to `fixtures.baseDir`) instead of next to each fixture. `factory.output.ignored`: see "Ignored outputs".
- `factory.options`: `dataset` takes `format: yaml` (the default), `xml` (flat XML) or `csv` (a directory with one CSV per table); `xml` takes `root` when the XSD declares several elements.
- `defaults:`: the lowest-priority values, under the prototype. Keys are exact paths (`recipient.country`), `[]` paths that fill every list element (`lines[].currency`), or bare field names that fill the field wherever nothing else does. For datasets, `table.column`.
- `prototype:` inline, for a small factory; prefer the sibling `<name>.prototype.yaml` (declaring both is an error).
- `fixtures:` inline fixtures (name to data), for a small factory; most fixtures are their own files.

### Families

| Family | Generates | Schema |
| --- | --- | --- |
| `avro` | Kafka payload JSON | an `.avsc` file |
| `json` | any JSON: request payloads, mock bodies | a JSON Schema, or an OpenAPI component (`../openapi/parcels.yaml#/components/schemas/Parcel`) |
| `yaml` | YAML documents | the same as `json` |
| `xml` | XML documents | an `.xsd` |
| `protobuf` | canonical proto-JSON | a `.proto` or a descriptor set, with the message (`../proto/depot.proto#depot.Scan`) |
| `dataset` | SQL seeds: YAML datasets, flat XML, CSV directories | optional SQL DDL: a file or a directory of `*.sql` migrations, read without a database |

A JSON Schema can `$ref` other local files, relative to the schema that holds the `$ref` (`"$ref": "address.schema.json"`, `"$ref": "common/money.schema.json#/$defs/Money"`). Remote schemas are not fetched: save them next to yours.

### Which factory a fixture belongs to

A fixture needs no `factory:` when exactly one factory sits in its directory or the nearest directory above that has one. Otherwise name it: `factory: portal-parcels` finds the nearest `portal-parcels.factory.yaml` above; a path (`factory: seeds/portal/portal-parcels`) is relative to `fixtures.baseDir` and works from anywhere.

A `<factory>.prototype.yaml` in another directory layers over the factory's prototype for the fixtures beneath it (the nearest wins): returns can share a status without a factory of their own.

## Reference another fixture

A value can be another fixture, or part of one, with `$ref`: the path of its `*.fixture.yaml`, relative to the file that refers to it (a leading `/` starts at `fixtures.baseDir`, by default the directory of `axx.yaml`), then `#` and a JSON Pointer into it.

```yaml title="mocks/returns/return-express.fixture.yaml"
data:
  reference: PX-RET-0001
  sender:
    $ref: ../parcels/parcel-express.fixture.yaml#/sender    # one value of another fixture
  recipient:
    $ref: ../parcels/parcel-express.fixture.yaml#/recipient # a whole object
    city: Hamburg                                           # keys next to $ref override it
```

A reference sees the fixture as it is generated: its prototype, its defaults and the identities derived for it. Loops fail and name the loop.

## Identities

An identity is a field whose value must be unique across every fixture of every factory: a parcel reference, a scan id, a manifest line id.

```yaml
identity:
  - path: parcels.parcels.reference   # dotted, with indices: lines[0].id; datasets: table.column
    prefix: PX-WEB-                   # a fixture without a value gets prefix + its name
```

- A fixture that sets the value pins it (`PX-WEB-5141`). One that leaves it out gets one derived from its name (`PX-WEB-portal-track`): unique by construction.
- `derive: authored` requires every fixture to set its own value and only enforces uniqueness.
- `format: uuid-name-based` derives a UUID (version 5) from the name, for fields that must hold a UUID. `qualifier` tells several identities of one fixture apart.
- Two fixtures with one value fail generation (`AXX-E0902`) and name both.

For the families that generate JSON (`avro`, `json`, `protobuf`), each identity also becomes a lint rule in `axx-lint.generated.yaml`, covering the factory's output directories, so hand-written files next to them are checked too. Pull the rules into `axx lint` once the file exists (an include that is missing is an error):

```yaml title="axx.yaml"
lint:
  include: [axx-lint.generated.yaml]
```

`fixtures.lint.emit: false` turns the generated rules off; `fixtures.lint.output` moves the file.

## Adopt existing files

`axx fixtures adopt` turns hand-written files into a factory. The prototype takes the values every file shares; each file becomes a `*.fixture.yaml` with its differences. Before writing anything it generates the files again from the new spec, and refuses (`AXX-E0905`) unless every one comes out equal to its original. Always try it with `--dry-run` first:

```sh
axx fixtures adopt --family json --schema openapi/address.yaml#/components/schemas/Address \
  --files 'mocks/addresses/*.json' --factory address-bodies --dry-run
```

- `--family` is `avro` (the default), `json`, `yaml`, `protobuf` or `dataset`; `xml` files cannot be adopted yet. `--schema` and `--files` are relative to `fixtures.baseDir`; the factory is written next to the adopted files.
- **Identities are yours to declare.** The report lists identity candidates (string fields distinct in every file, with how many files they were compared across), and the factory lists them as comments. Declare the ones that really identify a fixture: `--identity reference` (repeatable). An email distinct in three examples is not an identity. Check the `prefix:` adoption gives each declared identity: a new fixture without a value gets that prefix and its name.
- `--prototype common` takes each field's most common value instead of only what every file shares.
- `--into <factory> --files '<glob>'` adds more files to an existing factory, with its prototype and identities.
- A refusal lists the paths that would change. Seed layouts keyed by document id often cause it: adopt the records as a factory of their own, then reference them from the layout with `$ref`.
- Files a factory already generates cannot be adopted again.

Then run `axx fixtures generate`. The files keep their names, so the features change nothing.

## Generate, check and clean

| Command | Does |
| --- | --- |
| `axx fixtures generate` | writes the files that changed, the manifest and the generated lint rules; `--dry-run` lists them |
| `axx fixtures check` | read only: every generated file equals what its factory generates, the manifest lists exactly what the factories produce, files matched by `conformance` rules pass their schema. Exit 1 on any failure |
| `axx fixtures clean` | deletes the ignored outputs (below); `--dry-run` lists them |
| `axx fixtures untrack` | `git rm --cached` for ignored outputs still in the index, once, after making outputs ignored |

Exit codes: 0 ok, 1 drift or a failed check or generation, 2 invalid configuration or spec files, 4 git unavailable (`untrack`). `axx fixtures check` prints `fix:` lines with each failure.

The tool only ever touches files `axx-fixtures.manifest.yaml` records, with their checksums. Commit the factories, the fixtures, the prototypes, the manifest and `axx-lint.generated.yaml`: they are what reviewers read.

### Ignored outputs

With `output: { ignored: true }` (in `axx.yaml`'s `fixtures:`, or one factory's `factory.output`), generated files are not committed: Axx writes an exact `.gitignore` next to them, and a fresh clone runs `axx fixtures generate` before `axx run`. `axx fixtures untrack` takes previously committed copies out of the index.

## Project settings

All optional, under `fixtures:` in `axx.yaml` (`axx schema` has every key):

```yaml title="axx.yaml"
fixtures:
  baseDir: .                          # where fixture paths resolve (default: the directory of axx.yaml)
  factories: ["**.factory.yaml"]      # where factories are found (the default)
  sources: ["../infra/wiremock"]      # more roots, e.g. mock bodies a container serves
  output: { ignored: true }
  conformance:                        # hold hand-written files to a schema too
    - name: seeds match the database schema
      filePatterns: ["seeds/*.yaml"]
      schemaType: dataset
      schemaRef: ../infra/postgres/init/01-schema.sql   # relative to baseDir
```

## CI

When the project has factories, check before generating, then run:

```sh
axx fixtures check      # the sources, the committed manifest and the generated files agree
axx fixtures generate   # writes the ignored outputs a fresh clone lacks
axx lint                # ids and keys that collide across files
axx run
```

A fresh clone checks clean: ignored outputs it lacks pass when the committed manifest records what the sources produce. A manifest that was not generated again after the sources changed fails; generating first would update it and hide that.

## When something fails

| Code | Means | Do |
| --- | --- | --- |
| `AXX-E0901` | a factory, fixture or prototype file is malformed or has no factory | the message names the file; `axx schema --kind ...` has the format |
| `AXX-E0902` | generation failed: a value breaks the schema, a required field has no value, an identity collides, a `$ref` finds nothing | fix the fixture, prototype or `defaults:` |
| `AXX-E0903` | a generated file was edited by hand | move the change into the fixture, or revert the file |
| `AXX-E0904` | check found drift or a stale manifest | change the sources, not the outputs, and run `axx fixtures generate` |
| `AXX-E0905` | adoption refused | fix what it names; `--dry-run` again |

`axx explain <code>` or `references/error-codes.md` has every code.

## Don't

- Don't copy a generated file to make a new one: add a fixture.
- Don't edit a generated file, or the manifest.
- Don't reuse a value an identity owns in a hand-written file: `axx lint` reports it.
- Don't make a factory for one file.

## References

- `references/spec-files.md`: the JSON Schemas of factory, fixture and prototype files.
- `references/error-codes.md`: every error code.
