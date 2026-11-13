# 0003: CLI contract: JSON envelope, exit codes, error codes

- Status: Accepted; amended 2026-11-12 (exit code 6)
- Date: 2026-09-23

## Decision

**Exit codes** (frozen):

| Code | Meaning |
|------|---------|
| 0 | success / all scenarios passed |
| 1 | scenario failures |
| 2 | usage or configuration error |
| 3 | undefined/ambiguous steps or lint errors |
| 4 | environment/lifecycle failure (apps, health checks, infrastructure) |
| 5 | reserved (was: plugin protocol error; see ADR 0009) |
| 6 | every scenario passed, but the coverage is under a minimum (amendment below) |
| 130 | interrupted |

**`--json`** is supported by every command and always prints one envelope:

```json
{"schemaVersion": 1, "command": "axx steps search", "ok": true, "data": {}, "errors": []}
```

Errors carry `code` (`AXX-Exxxx`, stable), `message`, optional `location {file,line,column}`,
`hint`, and a `docs` URL. Adding fields is non-breaking; removing or retyping fields requires a
new `schemaVersion`.

**Compact output** (`--compact`) prints failures only; it is enabled automatically when a coding
agent environment variable is present and stdout is not a terminal.

## Consequences

Scripts, CI and agents can rely on these across releases; changes need a new ADR.

## Amendment, 2026-11-12: exit code 6, coverage under a minimum

A run with coverage (`axx run --coverage`) measures the system under test, and axx.yaml can set
minimums for it. A run whose scenarios all passed but whose coverage is under a minimum exits 6,
a code of its own: 1 says a scenario failed, and CI and coding agents act differently on the two
(fix the behavior, or cover more of it). A failing scenario, an undefined step or an interruption
keeps its own code. Its run report has `result` `passed`, and the envelope `ok` `false`. The run
report gains a `coverage` object, an addition, so its `schemaVersion` stays 1. Only a run of every
scenario its configuration selects checks the minimums; a run of part of them reports its coverage
with `check` `skipped`.

