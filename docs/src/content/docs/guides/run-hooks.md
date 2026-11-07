---
title: Run your commands around axx's
description: Run a command of your project's before or after an axx command, from axx.yaml, like writing the schema your fixtures read before they are generated.
---

A **hook** runs a command of your project's before or after one of axx's. List hooks in `axx.yaml`, under `hooks.commands`:

```yaml title="axx.yaml"
hooks:
  commands:
    # Write the schema the parcels seeds are checked against, from the migrations.
    - before: fixtures generate
      command: ./scripts/write-schema.sh
    # Send the run's report to the depot team, whether the run passed or not.
    - after: run
      shell: true
      command: ./scripts/send-report.sh reports/report.html
```

- **`before` or `after`** names the axx command, by its words after `axx`: `fixtures generate`, `run`, `validate`. Every command has hooks but `pack`, `init`, `upgrade`, `install` and `version`, which manage axx and its packs.
- **`command`** is what the hook runs, from the directory of `axx.yaml`: a string, split into words without a shell, or a list. With `shell: true`, it runs through `/bin/sh -c` (`cmd /C` on Windows).
- **A hook that fails before its command stops it.** The command doesn't run, and the error says which hook failed and how.
- **A hook after its command runs whether the command succeeded or failed.** When it fails, a command that succeeded fails too; after a command that failed, it is a warning.
- **A hook knows what it runs around:** `AXX_COMMAND` holds the command's words (`fixtures generate`), and an after hook's `AXX_COMMAND_FAILED` is `true` or `false`. It runs with the rest of the environment axx has.
- **A hook before `axx run` can tell the scenarios properties,** as a service does: lines `name=value` in the file `$AXX_PROPERTIES` names, which they read as `${sys:name}` ([Run beside your own stack](/guides/run-beside-your-stack/#what-axx-does-with-what-its-told)).
- **Its output goes to axx's stderr,** so the command's own output, its `--json` answer included, stays as it is.
- **Several hooks of one command** run in the order `axx.yaml` lists them.

A hook that names a command axx has not got stops every command that has hooks, so a typo can't go unnoticed. `axx doctor` still runs: it lists the project's hooks, and says which one is wrong.

## Hooks of packs

A pack can run hooks of its own around axx's commands. They run around those of `axx.yaml`: a pack's before hooks first, its after hooks last. `axx doctor` lists them with the others. To write one, see [Run something before or after a command](/guides/write-custom-steps/#run-something-before-or-after-a-command).
