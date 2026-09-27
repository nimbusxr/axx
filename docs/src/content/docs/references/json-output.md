---
title: JSON output
description: The --json envelope every Axx command prints, the error objects inside it, and the run report that axx run and the agent reporter produce.
---

Every command accepts `--json` and then prints exactly one JSON document on stdout: the **envelope**. It is a frozen contract ([ADR 0003](https://github.com/nimbusxr/axx/blob/main/docs/adr/0003-cli-contract.md)): fields may be added, but removing or retyping a field requires a new `schemaVersion`.

## The envelope

```json
{
  "schemaVersion": 1,
  "command": "axx steps search",
  "ok": true,
  "data": { }
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `schemaVersion` | number | Envelope version; currently `1`. |
| `command` | string | The command that ran, such as `axx run` or `axx steps search`. |
| `ok` | boolean | `true` when the command succeeded (exit code `0`). |
| `data` | object | The command's result. Absent when the command could not produce one. |
| `errors` | array | Errors that stopped the command. Absent when there are none: a run whose scenarios fail has `ok: false` and its report in `data`, without `errors`. |

The process [exit code](/references/error-codes/#exit-codes) is the same with or without `--json`.

## Errors

```json
{
  "schemaVersion": 1,
  "command": "axx run",
  "ok": false,
  "errors": [
    {
      "code": "AXX-E0202",
      "message": "invalid tag expression \"@x and\": Tag expression \"@x and\" could not be parsed because of syntax error: Expected operand.",
      "hint": "use expressions like \"@smoke and not @wip\"",
      "docs": "https://axx.nimbusxr.us/references/error-codes/#axx-e0202"
    }
  ]
}
```

| Field | Meaning |
| --- | --- |
| `code` | A stable `AXX-Exxxx` code; see [error codes](/references/error-codes/). Branch on this, not on `message`. |
| `message` | What went wrong, for humans. |
| `location` | Optional `{file, line, column}`, for example a line in `axx.yaml` or a feature file. |
| `hint` | Optional: what to do about it. |
| `docs` | A link to the code's entry in the error code reference. |

## The run report

`axx run --json` puts the run report in `data`. `--format agent:FILE` writes the same object to a file. Only failures are listed, so the size depends on what broke, not on the size of the suite. This example leaves out the failure's `logs` and `context`:

```json
{
  "schemaVersion": 1,
  "axx": "0.1.0",
  "result": "failed",
  "durationMs": 7,
  "counts": {
    "scenarios": { "failed": 1, "passed": 1 },
    "steps": { "failed": 1, "passed": 5 }
  },
  "notRun": 0,
  "interrupted": false,
  "failures": [
    {
      "scenario": "fails",
      "location": "features/demo.feature:6",
      "status": "failed",
      "step": {
        "keyword": "Then",
        "text": "the response status code is 201",
        "location": "features/demo.feature:9",
        "definition": "rest.response.status"
      },
      "error": {
        "kind": "assertion",
        "message": "Expected status code <201> but was <200>.",
        "expected": 201,
        "actual": 200
      },
      "rerun": "axx run features/demo.feature:6"
    }
  ],
  "undefined": []
}
```

| Field | Meaning |
| --- | --- |
| `result` | the worst scenario status: `passed`, `skipped`, `pending`, `undefined`, `ambiguous` or `failed`; `interrupted` when the run was cancelled |
| `durationMs` | wall-clock time of the run |
| `counts` | scenarios and steps by status (`passed`, `failed`, `skipped`, `undefined`, `ambiguous`, `pending`); zero counts are omitted |
| `notRun` | selected scenarios that did not run, for example after `--fail-fast` or an interrupt |
| `interrupted` | `true` when the run was cancelled |
| `dryRun` | `true` for `axx run --dry-run`; absent otherwise |
| `failures[]` | one entry per failed, undefined, ambiguous or pending scenario, in feature file order |
| `failures[].location`, `.tags`, `.rerun` | where the scenario is, its tags, and the command that reruns only it |
| `failures[].step` | the step that failed: keyword, text, location and the id of the matched `definition`; for a hook, `hook` is `before` or `after` |
| `failures[].error.kind` | `assertion`, `error`, `timeout`, `panic`, `undefined`, `ambiguous` or `pending` |
| `failures[].error.expected`, `.actual`, `.diff` | for assertions: the two values, as JSON, and a line diff when they are JSON documents or text of several lines |
| `failures[].error.stack`, `.candidates`, `.suggestions` | the stack of a panic, the matching definitions of an ambiguous step, close expressions for an undefined one |
| `failures[].logs`, `.context` | the scenario's logs (the last 50), and pack context such as the last HTTP request and response |
| `undefined[]` | undefined steps with suggestions, for writing them correctly |
| `runErrors[]` | problems found after the scenarios, outside any of them (`source`: the pack, `message`); present only when there are some, and they make `result` `failed` |

## Other commands

Each command documents its own `data`. The most useful for scripts and agents:

| Command | `data` |
| --- | --- |
| `axx steps search <q>` | `{query, steps[]}`; each step has `id`, `pack`, `expr`, `variants`, `keyword`, `arg`, `doc`, `examples`, `params` |
| `axx validate` | `{files, scenarios, steps, problems[], warnings[]}`; each problem has `kind` (`undefined`, `ambiguous`, `argument`), `location`, `text`, and a `message`, `suggestions` or `candidates` where they apply; `warnings[]`, present when there are some, are findings of `axx lint`'s feature checks (kind `lint`), which do not change the exit code |
| `axx doctor` | `{version, config, checks[]}`; each check has `name`, `status` (`ok`, `warn`, `fail`), `detail`, `hint` |
| `axx lint` | `{baseDir, rules[], summary, notes[], hints[]}`; `hints[]`, present when there are some, suggest fixture factories and never change the exit code |
| `axx init` | `{dryRun, files[], detected, agents}`; each file has `path`, `action` (`create`, `update`, `skip`), `reason`; `agents`, absent with `--no-agents`, has `detected[]` (agent ids), `skills[]` (like `files`) and `mcp[]` |
| `axx mcp install` | `{dryRun, change}`; `change` and each entry of `axx init`'s `agents.mcp[]` has `agent`, `scope` (`project`, `user`), `path`, `action` (`create`, `update`, `skip`, or `manual` when `command` adds the server), `reason`, `command` |
| `axx docs export` | `{files[]}` |

For editors and generated clients, the `axx.yaml` schema is at [`/schemas/v0/axx.schema.json`](/schemas/v0/axx.schema.json).
