---
title: Choose packs
description: Pick the packs a project uses with axx-packs.yaml and axx pack add, remove, list and update.
---

A **pack** is a set of steps. Axx publishes `rest`, `mock`, `sql`, `mongo`, `kafka` and `logs`. It also publishes packs for cloud services, one per service, named after its cloud ([Test cloud services](/guides/test-cloud-services/)):

- **AWS:** `aws-s3`, `aws-sqs`, `aws-sns`, `aws-eventbridge` and `aws-dynamodb`, which build on `aws-core`.
- **Google Cloud:** `gcp-storage`, `gcp-pubsub`, `gcp-bigquery` and `gcp-firestore`, which build on `gcp-core`.
- **Azure:** `azure-blob` and `azure-servicebus`.

A project can write packs of its own, or use packs other teams publish. Every pack, Axx's or anyone else's, is used the same way: the project lists it in `axx-packs.yaml`, next to `axx.yaml`.

## Choose the packs a project uses

```console
$ axx pack add rest sql
created axx-packs.yaml
added rest, sql
$ axx pack list
  rest                                     axx       46 steps
  sql                                      axx       17 steps
  mock                                     axx       not used
  mongo                                    axx       not used
  ...
```

```yaml title="axx-packs.yaml"
packs:
  - rest
  - sql
```

Only the listed packs are loaded, so their steps are the only ones `axx validate`, `axx steps` and the skills know about. Listing an AWS or Google Cloud pack loads its core too. `axx pack remove` takes packs off the list. `axx init` starts the file with `rest`, which its first feature uses. Commit `axx-packs.yaml`.

The first `axx` command that needs the project's steps prepares Axx with its packs:

```console
$ axx validate
axx: preparing rest, sql (once; cached for later runs)
axx: ready in 31s
2 files, 6 scenarios, 41 steps: ok
```

This happens once for each list of packs. Later runs start immediately. The first time, Axx downloads what it needs, so it takes longer. In CI, keep Axx's cache between runs ([Run in CI](/guides/run-in-ci/#keep-axxs-cache)).

## Add your own packs

Your own packs are listed the same way, by path or by Go module:

```console
$ axx pack new ./steps                          # create a pack in this repository
$ axx pack add github.com/team/axx-grpc@v1.2.0  # use a pack published as a Go module
```

[Write custom steps](/guides/write-custom-steps/) shows what goes in a pack. When you change a pack in the repository, the next `axx` command prepares Axx with the new version.

Versions of module packs are pinned in `axx-packs.lock` (commit it). `axx pack update` moves them to the latest version, or to the version written in `axx-packs.yaml`, and prepares Axx again.
