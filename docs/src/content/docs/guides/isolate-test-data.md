---
title: Isolate test data
description: Use axx lint to guarantee that ids, keys and names in seeds, payloads and features never collide, so scenarios can share infrastructure and run in parallel.
---

Scenarios run in parallel against one database, one broker and one set of mocks. They stay independent only if each one owns its data. Two seed files that both insert the parcel `PX-KES-1001`, or two scenarios that both publish events keyed `PX-TRK-3002`, produce failures that come and go with scheduling. `axx lint` finds those collisions before a run does.

## Write rules

Rules live under `lint:` in `axx.yaml`. Each one extracts values from files and says how unique they must be:

```yaml title="axx.yaml"
lint:
  config:
    baseDir: .          # patterns are relative to this directory
    mode: error         # error fails the lint; warn only reports

  rules:
    - name: Parcel references in seeds
      filePatterns: ["seeds/*.yaml"]
      regex: '^\s+-?\s*reference:\s*"([^"]+)"'
      description: Every seeded parcel and manifest line has its own reference
      validation: cross-file-unique

    - name: Manifest line ids
      filePatterns: ["seeds/*.yaml"]
      regex: '^\s+-?\s*id:\s*"([^"]+)"'
      validation: cross-file-unique

    - name: Depot scan ids
      filePatterns: ["seeds/*.json"]
      type: jsonpath
      jsonPath: "scans[*].scanId"
      validation: cross-file-unique
```

Each rule key is described in the [configuration reference](/references/config/#lint).

Generated rules can be merged in with `include`:

```yaml title="axx.yaml"
lint:
  include: [axx-lint.generated.yaml]   # written by `axx fixtures generate`
```

## Run it

```sh
axx lint
```

```console
FAIL Parcel references in seeds (cross-file-unique, 11 files): 1 duplicate value
     Every seeded parcel and manifest line has its own reference
     value "PX-KES-1001" appears in 2 files (cross-file-unique) [AXX-E0820]
       seeds/manifest-kestrel-resend.yaml:4:17  reference: "PX-KES-1001"
       seeds/manifest-kestrel.yaml:4:17         reference: "PX-KES-1001"
ok   Manifest line ids (cross-file-unique, 11 files)
ok   Depot scan ids (cross-file-unique, 1 file)
ok   kafka/depot-scans.factory.yaml: scanId uniqueness (cross-file-unique, 2 files)
ok   SQL selection and trigger ordinals (8 files)
axx lint: 5 rules, 22 files: 1 error, 0 warnings
```

A new seed file reused a reference that `seeds/manifest-kestrel.yaml` already inserts. The rule from `axx-lint.generated.yaml` and the built-in check of SQL ordinals in features run too.

`axx lint` exits with `3` when an `error`-mode rule finds a duplicate, like `axx validate` does for undefined steps. Run it in CI next to `axx validate`, and start new rules in `warn` mode while you clean up existing data.

## Tips

- **Prove a rule bites.** After writing a rule, plant a duplicate value and check that `axx lint` fails. A pattern that matches no files still passes (its line only says `no files matched`).
- **Name values after the scenario.** `PX-DBF-3001` tells a reader which feature owns it; `test` tells nobody anything.
- **Declare identities once.** With [fixture factories](/guides/fixture-factories/), an `identity:` in the factory spec generates the matching lint rule for you.
- **Some scenarios cannot share.** A scenario that changes a table's triggers affects everyone. Tag it with a tag from `run.exclusive` so it runs alone ([Run in parallel](/guides/parallel-runs/)).

[Scenario isolation](/explanations/scenario-isolation/) explains the reasoning behind shared infrastructure with isolated data.
