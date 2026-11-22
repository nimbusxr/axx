# 0021: A pack can be experimental

- Status: Accepted
- Date: 2026-11-22

## Context

Everything axx adds is under its promises from the day it lands. A step's text is frozen in
`testdata/steps.json`, an error code in `testdata/cli-contract.json`, a setting in the schemas
(ADR 0003, ADR 0004). That is right for what a suite builds on, and it leaves no room for a
feature whose shape is not known yet: the only way to learn whether a new feature is right is
for projects to use it, and by then it could no longer change.

The first feature that needs the room is security controls: scenarios marked with the controls
they prove, and a report of which required controls are covered. Its settings, its commands'
output and its evidence file will change as security teams and auditors use them.

## Decision

A pack can say that it is experimental (`core.Manifest.Experimental`).

- **Adding the pack is the opt-in.** A new feature reaches axx as a pack, and a project adds a
  pack by name (`axx-packs.yaml`). That is already a choice made on purpose, so nothing else is
  set: there is no list of experimental features in `axx.yaml`.
- **axx says what the pack is, where a project meets it:** as the pack is added (`axx pack
  add`), once for each run that uses it and for each of its own commands (on standard error),
  and wherever packs are listed (`axx pack list`, `axx pack show`, the packs' reference pages,
  `internal/packref/packs.json`, the MCP server's list of packs).
- **What an experimental pack may change, in any release, without a deprecation:** its steps,
  its settings (`packs.<name>`, which the schema of `axx.yaml` does not describe), its commands
  and what they print, and the files it writes.
- **What it may not add:** an exit code, or a field of the `--json` envelope. Programs branch on
  both, and neither can be taken back (ADR 0003). A feature gets those when it becomes stable.
- **Its error codes are frozen as any other code.** A code that goes with the feature is
  retired, which the contract has always allowed.
- **A pack becomes stable by dropping the mark.** No project's file names the stage, so no
  project's file changes.

A project's own pack can be experimental in the same way.

## What is not decided

An experimental step inside a stable pack, and an experimental flag or setting of axx itself.
Neither can be opted into by adding a pack: they would need a list in `axx.yaml`, and an
exemption for the step in the frozen catalog (`internal/engine/catalog_test.go`). No feature
needs them yet, and the first that does decides how.

## Consequences

- A pack of axx's that has steps cannot be experimental as things are: its steps would be
  frozen at once. The first experimental packs have none. The catalog's exemption is built with
  the first experimental pack that has steps.
- The docs have a page for the stages ("Stability"), beside the deprecation policy.
