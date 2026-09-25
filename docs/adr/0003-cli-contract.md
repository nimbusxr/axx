# 0003: CLI contract: JSON envelope, exit codes, error codes

- Status: Accepted
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
