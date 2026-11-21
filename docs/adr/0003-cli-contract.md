# 0003: CLI contract: JSON envelope, exit codes, error codes

- Status: Accepted; amended 2026-11-12 (exit code 6), 2026-11-21 (the codes are held by a test)
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

## Amendment, 2026-11-21: the codes are held by a test

"Frozen" was a rule people kept. A test now keeps it: `testdata/cli-contract.json` records each
exit code's number, each `AXX-E` error code, and the exit code an error ends its command with,
and `TestCLIContract` fails when one of them changes or goes.

- A new code is added to the file with `-update`, which only adds. A new exit code still needs an
  ADR first.
- An error code axx stops reporting is retired with `-retire`, the owner's decision. It stays in
  the file, and the test refuses a catalog entry with a retired code: a code is never given
  another meaning.
- What a code means is in its catalog entry, for people to review; the test holds what programs
  branch on.
