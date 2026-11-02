# Fixture factories in detail

## Contents
- The factory: its keys
- Families: what each generates, from which schema
- Which factory a fixture belongs to
- Reference another fixture ($ref)
- Identities
- Adopt existing files
- Generate, check and clean; ignored outputs
- Project settings, CI, error codes

## The factory

- `factory.family` and `factory.schema`: what it generates and what checks it (table below). The schema path is relative to the factory file.
- `factory.output.dir`: write every output there (relative to `fixtures.baseDir`) instead of next to each fixture. `factory.output.ignored`: see "Ignored outputs".
- `factory.options`: `dataset` takes `format: yaml` (the default), `xml` (flat XML) or `csv` (a directory with one CSV per table); `xml` takes `root` when the XSD declares several elements.
- `defaults:`: the lowest-priority values, under the prototype. Keys are exact paths (`recipient.country`), `[]` paths that fill every list element (`lines[].currency`), or bare field names that fill the field wherever nothing else does. For datasets, `table.column`.
- `prototype:` inline, for a small factory; prefer the sibling `<name>.prototype.yaml` (declaring both is an error).
- `fixtures:` inline fixtures (name to data), for a small factory; most fixtures are their own files.

## Families

| Family | Generates | Schema |
| --- | --- | --- |
| `avro` | Kafka payload JSON | an `.avsc` file |
| `json` | any JSON: request payloads, mock bodies, messages | a JSON Schema, an OpenAPI component (`../openapi/parcels.yaml#/components/schemas/Parcel`), or an AsyncAPI message's payload (`../asyncapi.yaml#/components/messages/scan`) |
| `yaml` | YAML documents | the same as `json` |
| `xml` | XML documents | an `.xsd` |
| `protobuf` | canonical proto-JSON | a `.proto` or a descriptor set, with the message (`../proto/depot.proto#depot.Scan`) |
| `dataset` | SQL seeds: YAML datasets, flat XML, CSV directories | optional SQL DDL: a file or a directory of `*.sql` migrations, read without a database |

A JSON Schema can `$ref` other local files, relative to the schema that holds the `$ref` (`"$ref": "address.schema.json"`, `"$ref": "common/money.schema.json#/$defs/Money"`). Remote schemas are not fetched: save them next to yours.

## Which factory a fixture belongs to

A fixture needs no `factory:` when exactly one factory sits in its directory or the nearest directory above that has one. Otherwise name it: `factory: portal-parcels` finds the nearest `portal-parcels.factory.yaml` above; a path (`factory: features/portal-parcels/seeds/portal/portal-parcels`) is relative to `fixtures.baseDir` and works from anywhere.

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

An identity is a field that tells fixtures apart: a parcel reference, a scan id, a manifest line id. No two fixtures, of any factories, have the same value in identity fields of the same name (the path's last field).

```yaml
identity:
  - path: parcels.parcels.reference   # dotted, with indices: lines[0].id; datasets: table.column
    prefix: PX-WEB-                   # a fixture without a value gets prefix + its name
```

- A fixture that sets the value pins it (`PX-WEB-5141`). One that leaves it out gets one derived from its name (`PX-WEB-portal-track`): unique by construction.
- `derive: authored` requires every fixture to set its own value and only enforces uniqueness.
- `format: uuid-name-based` derives a UUID (version 5) from the name, for fields that must hold a UUID. `qualifier` tells several identities of one fixture apart.
- Two fixtures with one value in fields of one name fail generation (`AXX-E0902`) and name both. Fields of different names, and two identities of one fixture, may share a value.
- `namespace:` compares identities by what they identify instead of by field name: different namespaces keep fields of one name apart (a depot's `id` and a shop's `id`), and one namespace compares fields of different names.

Each identity also becomes a lint rule in `axx-lint.generated.yaml`, covering the factory's output directories in the format the factory writes (JSON, YAML, XML, or a seed's YAML, flat XML or CSV), so hand-written files next to them are checked too. Pull the rules into `axx lint` once the file exists (an include that is missing is an error):

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
| `axx fixtures explain <file> <path>` | read only: where one value of a generated file comes from (the fixture, a prototype overlay, the prototype, `defaults:` or the schema's default, through any `$ref`, expression or identity), with the file and the place in it to edit. `--json` lists the origins |
| `axx fixtures clean` | deletes the ignored outputs (below); `--dry-run` lists them |
| `axx fixtures untrack` | `git rm --cached` for ignored outputs still in the index, once, after making outputs ignored |

Exit codes: 0 ok, 1 drift or a failed check or generation, 2 invalid configuration or spec files, 4 git unavailable (`untrack`). `axx fixtures check` prints `fix:` lines with each failure.

The tool only ever touches files `axx-fixtures.manifest.yaml` records, with their checksums. Commit the factories, the fixtures, the prototypes, the manifest and `axx-lint.generated.yaml`: they are what reviewers read.

## Ignored outputs

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

`axx explain <code>` explains every code.
