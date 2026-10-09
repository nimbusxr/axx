# 0013: On macOS, axx runs desktop scenarios under a launcher of its own, the one identity a person allows

- Status: Proposed (a draft: it waits on NimbusXR's Developer ID)
- Date: 2026-10-09
- Amends: [0011](0011-desktop-apps-through-the-accessibility-tree.md) (its checks before a run, and CI)

## Context

macOS asks for a permission (Accessibility, Screen Recording, Automation of another app) on
behalf of a process's **responsible process**: the app at the top of the process's tree. For axx
today that is whatever started it:

- the terminal or the IDE (Terminal, iTerm, IntelliJ, VS Code), each allowed on its own;
- on a hosted CI runner, bash, which the runner starts responsible for itself;
- and, for the app under test, the app itself. axx opens an app with `open`, and LaunchServices
  makes an app it opens responsible for itself. So the app has permissions of its own to ask
  for, apart from axx's.

What that cost, testing Snap (a screenshot tool, a Tauri app) on GitHub's macOS runner,
2026-10-07 to 10-09:

- **Snap asked, and no one could answer.** Opened with `open`, Snap asked for Automation (of
  System Events and of Preview) and for Screen Recording. Its CI job had to write rows into the
  runner's TCC databases for Snap's bundle identifier.
- **macOS 15 asked about bash.** Its alert, "bash is requesting to bypass the system private window
  picker", took keys and clicks meant for the app. An approval keyed by `/bin/bash` did not stop
  it. It still takes the front now and then: in one run, five scenarios in a row failed with
  "the backdrop app does not come to the front".
- **On a developer's Mac, every host asks again.** Each terminal and each IDE is allowed on its
  own. A build of the app under test that is not signed with a stable identity loses its grants
  every time it is built.
- **One machine already does better.** The self-hosted Mac mini runner (`mac-mini.yml`) runs its
  jobs under a small signed launcher that its LaunchAgent starts. The launcher is the one
  identity it allows, and that works because launchd starts the launcher at the top of the
  tree. Under a terminal or an IDE, the app above it would stay responsible.

axx itself cannot be that identity. The axx that runs a project's scenarios is built on the
developer's machine with the project's packs (ADR 0009), so it cannot carry NimbusXR's
signature. A launcher that rarely changes can.

## Decision (proposed)

On macOS, axx runs its desktop scenarios under **axx's launcher**: a small program, signed with
NimbusXR's Developer ID in an app of its own. A person allows it once, and it covers axx and
every app axx runs, whatever started axx.

1. **The handoff.** axx starts the launcher with `responsibility_spawnattrs_setdisclaim`, as
   Chromium and VS Code start their helper processes. The launcher is then responsible for itself
   and for every program under it, whatever terminal, IDE or CI agent started axx.
   (`internal/launcher.Start`.)
2. **The launcher runs axx as its child.** `axx-launcher program [arguments...]` passes standard
   input and output through, passes stop signals on, and returns the program's exit status
   (`internal/launcher.Run`, `internal/tools/axx-launcher`). axx runs itself again under it for a
   desktop run on macOS, and the second run knows it is under the launcher.
3. **Apps under test start as axx's children**, from their bundle's executable, so the
   launcher's grants are theirs (the owner's decision, 2026-10-08: "we need to run the apps as
   children"). `open` stays only for Apple's own apps, which macOS stops when they are started
   from their executable.
4. **It is signed with NimbusXR's Developer ID and notarized, and nothing else signs it** (the
   owner's decision, 2026-10-08: never a personal certificate, not even for now). The release
   workflow builds it into an app (`axx.app`, its identifier to settle; proposed
   `us.nimbusxr.axx.launcher`), signs, notarizes and publishes it. axx downloads it on first use,
   as it downloads OpenH264, and checks its signature and team. A grant is kept by the
   signature, so it lasts across axx's updates.
5. **`axx doctor`, and the first desktop step of a run, check the launcher's grants**: one
   identity, not "the app at the top of the process tree, which the hint names".
6. **In CI, the launcher is the one identity a hosted macOS runner allows**: its TCC rows and its
   screen-capture approval, in place of rows for each app under test and for bash. Writing them
   needs the owner's approval, as Snap's did.

## Evidence: the prototype, 2026-10-08, on the owner's MacBook

Who macOS holds responsible, asked with `responsibility_get_pid_responsible_for_pid`. The test was a
program that reports on itself and on a child it starts:

| How the program started | The program is charged to | Its child is charged to |
|---|---|---|
| as axx is today, from a shell | itself | itself |
| under a plain launcher (the Mac mini's) | itself | itself |
| under a launcher started with the handoff | the launcher | the launcher |
| as above, the child being Snap's own executable | the launcher | the launcher |

- **The handoff works from pure Go:** purego binds the functions, and `CGO_ENABLED=0` holds. axx
  depends on purego already (the desktop pack's macOS driver).
- **Nothing a person sees changes.** Output, input, exit codes, Ctrl-C (exit 130) and the terminal
  itself (stdout stays a TTY) all pass through the chain.
- **One grant covers everything under the launcher.**
  - Before the owner allowed `axx-launcher-proto`, a program under it had neither Accessibility
    nor Screen Recording.
  - After one approval, it had both, and so did a program two levels below it.
  - `screencapture` under the launcher captured the screen.

This draft's tests (`internal/launcher`) repeat the handoff's proof on every run of CI's macOS job:
- a program started with the handoff is responsible for itself, and for the program it starts;
- a program started the usual way is not responsible for its children;
- `Run` passes an exit status, a signal's (128 plus its number) and Ctrl-C through.

## Options considered

- **Grants per host (today).** One for each terminal, IDE and CI agent, plus one for each app under
  test. CI writes rows for each app. The bash alert stays.
- **Signing axx itself.** The binary that runs a project's scenarios is built on the developer's
  machine with its packs, so it can't carry NimbusXR's signature.
- **A launcher without the handoff** (the Mac mini's). It covers what is under it only where launchd
  starts it. Under a terminal or an IDE, the app above stays responsible.
- **Opening `axx.app` with `open`.** LaunchServices would make it responsible, but it would not be
  the terminal's child: input, output, the exit status and Ctrl-C would need a relay. The handoff
  keeps them as they are.

## Plan

- [x] Prove the handoff and the launcher: the prototype above, and this draft (`internal/launcher`,
  `internal/tools/axx-launcher`, their tests).
- [ ] **The owner:** NimbusXR in the Apple Developer Program, as an organization (in progress); a
  Developer ID Application certificate; an App Store Connect API key for notarization. Their
  secrets go in a protected `release` environment.
- [ ] Release: build `axx-launcher` into `axx.app`, sign it, notarize and staple it, and publish it
  with each release.
- [ ] axx downloads it on first use, checks its signature and team, and keeps it in its cache.
- [ ] axx runs a macOS desktop run under it.
- [ ] desktop-macos starts apps from their bundle's executable, and opens only Apple's apps with
  `open`.
- [ ] `axx doctor` and the first desktop step check the launcher's grants.
- [ ] CI: the hosted macOS recipe (the launcher's TCC rows and approval), with the owner's
  approval. Snap's CI then grants the launcher instead of Snap and bash.
- [ ] Docs: the desktop guide's permissions, and ADR 0011's checks and CI.

## Open questions

1. **Its name**, which a person sees in System Settings, and its identifier, which keeps its grants
   and so is chosen once. Proposed: "axx", `us.nimbusxr.axx.launcher`.
2. **Where axx runs itself again under it.**
   - In the core (`cmd/axx`), before a run: the core binary would carry purego, which is pure Go.
   - Or in the project's build, which has the desktop packs.
3. **A way out**, for someone who prefers their terminal's grants: `AXX_LAUNCHER=off`.
4. **The private API.** If a macOS release removes it, `Start` fails as the library loads, and axx
   runs as it does today, with the host's grants; `axx doctor` says so.

## Consequences

- One grant per Mac, kept across axx's updates, that works from any terminal, IDE or CI agent.
- The apps under test share it, as axx's children.
- CI allows one identity. There are no rows per app under test, and no bash alert.
- axx's release signs and notarizes a macOS app under NimbusXR's Apple account.
- axx relies on a private macOS API that Chromium and VS Code also rely on, and falls back to
  today's behaviour without it.
- Apps start from their executable rather than through LaunchServices. Snap ran that way in the
  prototype; an app that needs LaunchServices (one that expects to be opened by its bundle) will
  show which.
