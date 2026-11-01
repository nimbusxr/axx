---
name: axx-test-data
description: Keep the test data of axx acceptance tests - SQL and MongoDB seeds, Kafka events, request payloads, mock bodies - in fixture factories that check it against its schema and keep its ids unique. Covers axx fixtures (generate, check, adopt, explain, clean), factories, prototypes, fixtures, $ref, identities, generated lint rules and axx lint. Use when several scenarios need payloads, seeds or mock bodies of one shape, or hand-written ones repeat.
license: Apache-2.0
---

# Test data with axx

Scenarios read their data from files (`Given a seeds/portal-find.yaml db seed`). Write one-off files by hand. **Fixture factories are optional**: use them where files of one shape repeat. A factory keeps the shared shape once and each fixture as only its differences; `axx fixtures generate` writes ordinary files, checked against their schema, with ids no other fixture has. Features cannot tell them from hand-written files, and nothing is generated during `axx run`.

Use a factory for three or more files of one shape (seeds of one table, events of one Avro record, bodies of one API response), or when a new scenario needs data like an existing file's. `axx lint` hints when hand-written files of one shape repeat; `axx fixtures adopt` turns them into a factory without changing a value (`--dry-run` first).

## The routine

1. Look for a factory first: `*.factory.yaml`, next to the files it generates; `axx-fixtures.manifest.yaml` lists every generated file.
2. Add a fixture: `<name>.fixture.yaml` next to the factory, with only what differs from the prototype. Its name is the generated file's.
3. Generate: `axx fixtures generate`. A value that breaks the schema, or an id another fixture has, fails it, naming the fixture and the field.
4. Use the generated file in the step, as a hand-written one.
5. Check: `axx fixtures check` (nothing drifted), then `axx lint` (no id is in two files).

Never edit a generated file (generate refuses, `AXX-E0903`): change the fixture, prototype or factory and generate again. `axx fixtures explain <generated file> <path>` names what sets a value.

## The files

```text
features/portal-parcels/seeds/portal/
  portal-parcels.factory.yaml     # family, schema, identities
  portal-parcels.prototype.yaml   # the shape every fixture starts from
  portal-open.fixture.yaml        # a fixture: its differences
  portal-open.yaml                # generated
```

```yaml
# portal-parcels.factory.yaml
factory:
  family: dataset                                       # avro, json, yaml, xml, protobuf, dataset
  schema: ../../../../../infra/postgres/init/01-schema.sql   # relative to this file
identity:
  - path: parcels.parcels.reference                     # no two fixtures share a value
    prefix: PX-WEB-                                     # a fixture without one gets prefix + its name
```

```yaml
# portal-parcels.prototype.yaml
data:
  parcels.parcels:
    sender: lark-and-loom
    status: REGISTERED
```

```yaml
# portal-open.fixture.yaml
data:
  parcels.parcels:
    - reference: PX-WEB-5141
      status: IN_TRANSIT
```

Maps merge; lists and scalars replace. Each identity also becomes a rule in `axx-lint.generated.yaml`: include it with `lint: {include: [axx-lint.generated.yaml]}` once it exists.

`references/factories.md` has the rest: every factory key, the families and their schemas, `$ref` to another fixture, identities, adopting, ignored outputs, settings, CI and the error codes. `references/spec-files.md` has the files' JSON Schemas (`axx schema --kind factory|fixture|prototype` prints them).

## Don't

- Copy a generated file to make a new one (add a fixture), or edit a generated file or the manifest.
- Reuse in a hand-written file a value an identity owns: `axx lint` reports it.
- Make a factory for one file.
- Build what a cloud wraps (a Pub/Sub push envelope): publish the message, and the emulator delivers the envelope.
