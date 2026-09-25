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
  "data": { },
  "errors": []
}
```

| Field | Type | Meaning |
| --- | --- | --- |
| `schemaVersion` | number | Envelope version; currently `1`. |
| `command` | string | The command that ran, such as `axx run` or `axx steps search`. |
| `ok` | boolean | `true` when the command succeeded (exit code `0`). |
| `data` | object | The command's result. Absent when the command could not produce one. |
| `errors` | array | Errors that stopped the command. Absent or empty on success. |

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

`axx run --json` puts the run report in `data`. `--format agent:FILE` writes the same object to a file. Only failures are listed, so the size depends on what broke, not on the size of the suite.

```json
{
  "schemaVersion": 1,
  "axx": "0.1.0",
  "result": "failed",
  "durationMs": 7,
  "counts": {
    "scenarios": { "failed": 1, "passed": 1 },
    "steps": { "failed": 1, "passed": 2 }
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
| `result` | `passed` or `failed`, summarizing the run |
| `durationMs` | wall-clock time of the run |
| `counts` | scenarios and steps by status (`passed`, `failed`, `skipped`, `undefined`, `ambiguous`, `pending`); zero counts are omitted |
| `notRun` | selected scenarios that did not run, for example after `--fail-fast` or an interrupt |
| `interrupted` | `true` when the run was cancelled |
| `failures[]` | one entry per scenario that did not pass |
| `failures[].location`, `.rerun` | where the scenario is, and the command that reruns only it |
| `failures[].step` | the step that failed: keyword, text, location and the id of the matched `definition` |
| `failures[].error.kind` | `assertion`, `error`, `timeout`, `panic`, `undefined`, `ambiguous` or `pending` |
| `failures[].error.expected`, `.actual` | for assertions: the two values, as JSON |
| `failures[].logs`, `.context` | logs attached to the step, and pack context such as the last HTTP request and response |
| `undefined[]` | undefined steps with suggestions, for writing them correctly |
| `runErrors[]` | problems found after the scenarios, outside any of them (`source`: the pack, `message`); present only when there are some, and they make `result` `failed` |

## Other commands

Each command documents its own `data`. The most useful for scripts and agents:

| Command | `data` |
| --- | --- |
| `axx steps search <q>` | `{query, steps[]}`; each step has `id`, `pack`, `expr`, `variants`, `keyword`, `arg`, `doc`, `examples`, `params` |
| `axx validate` | `{files, scenarios, steps, problems[]}`; each problem has `kind` (`undefined`, `ambiguous`, `argument`), `location`, `text`, and `suggestions` |
| `axx doctor` | `{version, config, checks[]}`; each check has `name`, `status` (`ok`, `warn`, `fail`), `detail`, `hint` |
| `axx docs export` | `{files[]}` |

For editors and generated clients, the `axx.yaml` schema is at [`/schemas/v0/axx.schema.json`](/schemas/v0/axx.schema.json).
