# 0009: Custom steps are Go packs; the axx executable loads the packs a project uses

- Status: Accepted
- Date: 2026-09-24
- Supersedes: [0002](0002-step-protocol.md)

## Context

ADR 0002 let custom steps be written in any language through a JSON-RPC step protocol. The
scenario context, the per-scenario state the steps of every pack share (services, requests and
responses, selections, topic clients), is the backbone of a scenario. Reaching it from other
processes means serializing it and replicating live resources such as database connections
in every language, which is painful to build and to extend.

## Decision

- **Custom steps are written in Go**, as packs built on the same public core as axx's own
  packs. The other-language SDKs, the step protocol and the plugin host are withdrawn.
- **The `axx` binary is the core, and every pack is separate.** axx's packs and anyone else's
  load the same way, and their steps are used in feature files alike. A project uses only the
  packs it lists. Pack dependencies (database drivers, cloud SDKs) never enter the core binary.
- **The core and every pack are public Go packages**, and each pack exposes its context, so a
  custom pack works on the same objects the steps of axx's packs use.
- **A project lists its packs in `axx-packs.yaml`**, managed with `axx pack add|remove|list|update`
  and pinned in `axx-packs.lock`. axx's packs are added by name, other packs by path or
  module (`./steps`, `github.com/team/axx-grpc@v1`). A registry may resolve short names later.
- Go cannot reliably load compiled code into a running binary: plugins need cgo, do not work
  on Windows and require identical dependency versions. So axx builds itself with a project's
  packs, once per list of packs and axx version, caches the result and runs it in its place.
- axx builds with the Go release it was built with, which it downloads from go.dev on first
  use and checks against the published SHA-256. Users never install Go.
- Exec steps (a step in `axx.yaml` that runs a command) are removed as well; custom steps are
  Go packs only.

## Consequences

- One process, one context: no serialization of scenario state and no replicated connections.
- Exit code 5 (`plugin`) is no longer produced; the number stays reserved.
- The first run with a list of packs downloads the toolchain and the packs' modules and
  compiles them, which takes about half a minute; later runs start immediately. CI keeps the
  cache between runs.
- A project without `axx-packs.yaml` has no steps; `axx init` creates the file.
