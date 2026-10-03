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
FAIL Parcel references in seeds (cross-file-unique, 52 files): 1 duplicate value
     Every seeded parcel and manifest line has its own reference
     value "PX-KES-1001" appears in 2 files (cross-file-unique) [AXX-E0820]
       seeds/manifest-kestrel-resend.yaml:4:17  reference: "PX-KES-1001"
       seeds/manifest-kestrel.yaml:4:17         reference: "PX-KES-1001"
ok   Manifest line ids (cross-file-unique, 52 files)
ok   Depot scan ids (cross-file-unique, 1 file)
ok   kafka/depot-scans.factory.yaml: scanId uniqueness (cross-file-unique, 2 files)
ok   requests/registrations.factory.yaml: reference uniqueness (cross-file-unique, 2 files)
ok   SQL selection and trigger ordinals (13 files)
ok   REST request ordinals (13 files)
hint: seeds/ has 39 hand-written .yaml files of one shape (parcels.parcels) that no fixture factory generates; a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)
hint: seeds/ has 5 hand-written .yaml files of one shape (parcels.manifest_lines) that no fixture factory generates; a factory would keep what they share in one place (optional; `axx fixtures adopt --help`)
axx lint: 7 rules, 70 files: 1 error, 0 warnings
```

A new seed file reused a reference that `seeds/manifest-kestrel.yaml` already inserts. The rules from `axx-lint.generated.yaml` run too, and so do the built-in checks of the features: SQL selections and triggers, and REST requests, addressed by ordinals they cannot have (a `3rd ordered` request after only one, or a second request added without its ordinal). They are warnings, and `axx validate` reports them as well.

The `hint:` lines are suggestions, never errors or warnings. When the scenarios read three or more hand-written `.json` or `.yaml` files from one directory, whose top-level keys are the same and that no [fixture factory](/guides/fixture-factories/) generates, `axx lint` suggests one: it would keep what the files share in one place and each file's differences in its own. Factories are optional; `axx fixtures adopt` turns the files into one without changing a value.

`axx lint` and `axx validate` also hint at scenarios whose checks prove little: those that check only a success status (a 2xx response, or a command's exit code 0), which says it was accepted, not what it did, and those that check only that something did not happen, which also passes when the action never ran. They also name scenarios that check several things in turn (When … Then …, then When … Then … again): each acceptance criterion reads best as a scenario of its own. A hint names the scenarios; whether one needs another check, or splitting, is for its author to judge.

`axx lint` exits with `3` when an `error`-mode rule finds a duplicate, like `axx validate` does for undefined steps. Run it in CI next to `axx validate`, and start new rules in `warn` mode while you clean up existing data. `axx lint --mode error` makes every rule an error, the built-in checks of the features too, so CI fails on an ordinal that would fail every run, before any app starts.

## Tips

- **Prove a rule bites.** After writing a rule, plant a duplicate value and check that `axx lint` fails. A pattern that matches no files still passes (its line only says `no files matched`).
- **Name values after the scenario.** `PX-DBF-3001` tells a reader which feature owns it; `test` tells nobody anything.
- **Declare identities once.** With [fixture factories](/guides/fixture-factories/), an `identity:` in the factory spec generates the matching lint rule for you.
- **Some scenarios cannot share.** A scenario that changes a table's triggers affects everyone. Tag it with a tag from `run.exclusive` so it runs alone ([Run in parallel](/guides/parallel-runs/)).

[Scenario isolation](/explanations/scenario-isolation/) explains the reasoning behind shared infrastructure with isolated data.
