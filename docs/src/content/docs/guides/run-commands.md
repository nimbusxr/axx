---
title: Run commands
description: Run commands in scenarios, such as a command-line tool you ship or an operator's admin command, and check their exit code, what they print and what they print as errors.
---

Some of what a system does is done from the command line: an admin command an operations team runs where the service runs, a command-line tool you ship to customers, a migration or an export. The `cli` pack runs them in scenarios and checks how they end and what they print.

```gherkin
Scenario: The desk cannot cancel a parcel the courier has picked up
  Given a seeds/admin-picked-up.yaml db seed
  When the admin command is run with 'cancel PX-ADM-6102'
  Then the admin command's exit code is 3
  And the admin command's error output contains 'PX-ADM-6102 is IN_TRANSIT: it can no longer be cancelled'
  And the admin command's output is empty
```

Add the pack to the project with `axx pack add cli` ([Choose packs](/guides/use-packs/)).

## Register the command

Register the command under a name, usually in the `Background`:

```gherkin
Background:
  Given the admin command with the following properties:
    | command | docker compose exec -T app parcels admin |
    | dir     | ../infra                                 |
    | timeout | 30s                                      |
```

| Property | What it is |
| --- | --- |
| `command` | The program and its first arguments (required). They are split like an app's `command` in `axx.yaml`: double quotes group words, and there is no shell, so no pipes, redirects or `$VARIABLES`. |
| `dir` | The folder it runs in, relative to the directory of `axx.yaml`. By default, an empty folder of the scenario's own. |
| `env.<NAME>` | An environment variable, on top of those axx runs with. |
| `timeout` | How long it may run: `1m` unless it says otherwise. |

A program named by a relative path, such as `bin/parcels`, is found from the directory of `axx.yaml`. A bare name, such as `psql`, is looked up on the `PATH`. Values can use `${env:..}` and `${sys:..}`, and `${env:..}` values are masked in logs, attachments and failures.

A command that belongs to a service can run where the service runs: in its container, through `docker compose exec -T` (`-T` because no terminal is attached). The parcels example keeps the command in a property, so a run of the service on the host can replace it with `-D`:

```yaml title="axx.yaml"
properties:
  parcels.admin: docker compose exec -T app parcels admin
```

```gherkin
Given the admin command with the following properties:
  | command | ${sys:parcels.admin} |
  | dir     | ../infra             |
```

## Run it

Run the command with more arguments, which are split the same way, and, if it reads them, lines of input (its stdin):

```gherkin
When the admin command is run with 'label PX-ADM-6101'
```

```gherkin
When the admin command is run with 'cancel -' and the input:
  """
  PX-ADM-6103
  PX-ADM-6104
  """
```

A command that ends with a failing exit code does not fail the step that runs it: that is for the checks to say. Only a command that cannot start, or runs past its timeout, fails the step, and it shows what the command printed. A command that runs past its timeout is stopped with every process it started. A command that starts a process elsewhere, as `docker compose exec` does in a container, stops, but the process it started may run on.

## Check what it did

The checks look at the command's last run in the scenario:

```gherkin
Then the admin command's exit code is 0
And the admin command's output is:
  """
  cancelled PX-ADM-6103
  cancelled PX-ADM-6104
  """
```

| Check | What it compares |
| --- | --- |
| `exit code is {int}` | The exit code. |
| `output contains {string}` | Text anywhere in what it printed. Runs of spaces and line breaks count as one space; a failure shows the nearest line. |
| `output has a line matching {string}` | A line that matches the regular expression (Java syntax). |
| `output is:` | The whole of what it printed, with `\r\n` read as `\n` and one final line break left out. |
| `output is empty` | Nothing printed at all. |
| `output has the following properties:` | JSON it printed: a path and its value, like the other JSON property steps. |
| `output is identical to the {filepath} file` | Byte for byte, a file of the project. |

Each check but the last two also reads the error output (stderr): `the admin command's error output contains '...'`.

A command's JSON output is checked by its properties:

```gherkin
Then the admin command's output has the following properties:
  | shop                    | pebble-and-pine |
  | parcels[0].reference    | PX-ADM-6105     |
  | parcels[1].serviceLevel | EXPRESS         |
```

And output a printer or another system reads, such as a shipping label, is compared with a file:

```gherkin
Then the admin command's output is identical to the labels/PX-ADM-6101.zpl file
```

An exit code of 0 says the command succeeded, not what it did. Check what it did too: what it printed, or a row it changed. `axx lint` points out a scenario whose only check is a success.

## Keep scenarios apart

Scenarios run in parallel, so a command must not write over another scenario's files. By default a command runs in an empty folder of the scenario's own: the folder is removed when the scenario passes and kept when it fails, for you to look at, and the scenario's log says where it is. A command that works on shared data, like the admin command on the parcels database, gets data unique to the scenario, like every other step ([Isolate test data](/guides/isolate-test-data/)).

See the [cli pack's reference](/references/packs/cli/) for every step.
