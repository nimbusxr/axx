---
title: Clean up after runs
description: Remove what runs leave in a project (logs, videos, traces, reports) with axx clean, and see and trim what Axx keeps on the machine with axx cache.
---

Axx writes files in two places, and each has its own command:

| Where | What is there | Command |
| --- | --- | --- |
| The project's `.axx` folder | What runs leave: logs, videos, screenshots, traces, coverage, demo videos | `axx clean` |
| Axx's cache on the machine | What Axx would otherwise build or download again, for every project | `axx cache` |

Everything both commands remove comes back when a run needs it. Nothing they remove is a file you wrote.

## Remove what runs left in a project

1. See what would go:

   ```sh
   axx clean --dry-run
   ```

   ```text
   would remove 105 MB that runs left in this project:
     .axx/demo            101 MB
     .axx/web             3.7 MB
     .axx/logs            480 kB
     build/axx/junit.xml   12 kB
   ```

2. Remove it:

   ```sh
   axx clean
   ```

`axx clean` removes two things:

- Everything Axx and its packs wrote into the project's `.axx` folder.
- The report files that the reporters in `axx.yaml` name (`junit: build/axx/junit.xml`), where they are inside the project.

Two things in `.axx` stay, because they say what is running:

- `.axx/run` records the services that `axx up` keeps, and what an interrupted run left for `axx down` to stop.
- `.axx/host.json` is where the [Axx host](/guides/run-from-a-sandbox/) that serves the project announces itself.

`axx clean` refuses while the project is in use, because what is running writes into `.axx`:

- While `axx up` keeps services running. Stop them with `axx down` first.
- While a run is going on. Run `axx clean` again once it has ended.

`axx clean --dry-run` works in both cases, as it removes nothing.

:::note
A demo's last take is in `.axx/demo`. `axx demo --recut` and a live show's "play on from the last take" need it, so make the demo again after `axx clean` before you present ([Make a demo](/guides/make-a-demo/)).
:::

### Remove what your own tools leave too

To remove what your own tools write during runs, give the project a [hook](/guides/run-hooks/) that runs after `axx clean`:

```yaml title="axx.yaml"
hooks:
  commands:
    - after: clean
      command: rm -rf build/depot-reports
```

## See what Axx keeps on the machine

Axx's cache is one folder for all projects: `~/.cache/axx` on Linux, `~/Library/Caches/axx` on macOS, `%LocalAppData%\axx` on Windows. `axx cache` lists what is in it:

```sh
axx cache
```

```text
axx's cache: /home/ada/.cache/axx, 3.4 GB

  go        1.6 GB  the Go that builds packs, and the modules it downloaded
  builds    1.1 GB  axx as built with each project's packs
  web       420 MB  the web packs' tools: Playwright, Lighthouse, axe-core
  versions  210 MB  the releases of axx that projects name
  node      110 MB  the Node.js that runs Playwright and other tools

11 binaries of axx (800 MB) went unused for 30 days: `axx cache clean` removes them (axx does so by itself too, at most once a day).
```

## What Axx removes by itself

Two parts of the cache hold binaries of Axx, and they are the parts that grow:

- `builds` has Axx as built with each project's packs ([Choose packs](/guides/use-packs/)). Every release of Axx and every change to a project's packs adds one.
- `versions` has the releases that projects name in `axx.yaml` ([Keep a project on one version](/guides/install/#keep-a-project-on-one-version)).

The earlier ones are never asked for again. So Axx notes each time it starts one, and removes the ones that nothing started for 30 days. It checks at most once a day, when a command starts, and says nothing unless that takes more than a second. It never removes a binary that is running or being built.

To change this, set `AXX_CACHE_KEEP_DAYS`:

| Value | What Axx does |
| --- | --- |
| not set | Removes a binary that nothing used for 30 days |
| `7` | Removes a binary that nothing used for 7 days |
| `0` | Removes nothing by itself |

Two cases have rules of their own:

- **A cache that a CI job restores, or that an image has.** Its files are as old as the day it was saved, though the job uses them. Axx judges a binary as of the last time it checked that cache, not as of today, so a cache that was just restored loses nothing.
- **A machine with `AXX_AUTO_DOWNLOAD=0`.** Its releases were put there with `axx install`, so Axx removes none of them by itself.

## Remove more from the cache

Everything else in the cache stays until you remove it, because getting it again takes minutes: a Go toolchain, a browser's driver, a phone simulator.

1. Remove the binaries of Axx that nothing used for 30 days, without waiting for Axx to do it:

   ```sh
   axx cache clean
   ```

2. Remove whole parts, by the names `axx cache` lists:

   ```sh
   axx cache clean web node
   ```

3. Remove all of it:

   ```sh
   axx cache clean --all
   ```

Each form takes `--dry-run`, which says what would go and removes nothing. The next run builds or downloads what it needs again.

The browsers that the web packs drive are not in Axx's cache. Playwright keeps them in its own folder ([The browsers](/guides/test-web-apps/#the-browsers)).

## For agents

Both commands take `--json`. `axx clean --json` lists each path it removed, relative to the project, with its size in bytes:

```json
{
  "schemaVersion": 1,
  "command": "axx clean",
  "ok": true,
  "data": {
    "dryRun": false,
    "bytes": 4900009,
    "removed": [
      { "path": ".axx/web", "bytes": 4000000 },
      { "path": ".axx/demo", "bytes": 900000 },
      { "path": "build/axx/junit.xml", "bytes": 5 },
      { "path": ".axx/logs", "bytes": 4 }
    ]
  }
}
```

In a project that is in use, it fails with `AXX-E0419` and removes nothing.
