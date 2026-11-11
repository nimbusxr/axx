# 0016: On macOS, axx runs desktop scenarios under a launcher of its own, the one identity a person allows

- Status: Proposed (a draft). As of 2026-11-09, NimbusXR is in the Apple Developer Program, and
  its Developer ID certificate and notarization key are in the release environment.
- Date: 2026-10-09
- Revised: 2026-11-09. Numbered 0016, since main took 0013 meanwhile. It now covers the MCP server
  and the hand-overs ADR 0015 added, and it says how desktop-macos starts apps today.
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
   (`internal/launcher.Run`, `internal/tools/axx-launcher`). On macOS, axx runs itself again
   under it for every command that can drive Mac apps, and the second run knows it is under the
   launcher. Those commands are:
   - `axx run`;
   - `axx mcp`, because an agent's `steps_try` runs its steps inside the MCP server's own process
     (`internal/mcp/session.go`). Without the launcher, the desktop steps an agent tries would ask
     for the grants of whatever started the agent.

   axx runs itself again once, in its outermost process. `axx run --debug-steps` starts Delve,
   and Delve starts axx again (`internal/cli/debugsteps.go`). So the launcher goes above Delve,
   and the run Delve starts does not start a second launcher.
3. **Apps under test start as axx's children**, from their bundle's executable, so the
   launcher's grants are theirs (the owner's decision, 2026-10-08: "we need to run the apps as
   children"). `open` stays only for Apple's own apps, which macOS stops when they are started
   from their executable.

   Today desktop-macos opens a `.app` with `open -n -g -a` (`packs/desktop/macos/launch_darwin.go`),
   and LaunchServices makes it responsible for itself. Started from its executable instead, the
   app must still get everything `open` gives it now:
   - the environment that packs add when an app launches (`desktopcore.OnLaunch`, such as
     models-macos's library for a test build);
   - its arguments, its language and region, and window restoration turned off;
   - the documents it is asked to open. A Cocoa app opens the documents it is asked to open, not
     the ones among its arguments.

   An app asked to open documents still opens with `open`, which hands them to it, until axx
   has another way it has seen work. Without the launcher, apps open with `open` as before.
4. **It is signed with NimbusXR's Developer ID and notarized, and nothing else signs it** (the
   owner's decision, 2026-10-08: never a personal certificate, not even for now). The release
   workflow builds it into an app (`axx.app`, shown as "axx", its identifier
   `us.nimbusxr.axx.launcher`: the owner's decision, 2026-11-09), signs, notarizes and publishes
   it. Signing runs on a GitHub-hosted macOS runner, never on the Mac mini, which runs pull
   requests' code and so must never hold the release's secrets. axx downloads it on first use,
   as it downloads OpenH264, and checks its signature and team. A grant is kept by the
   signature, so it lasts across axx's updates.
5. **`axx doctor`, and the first desktop step of a run, check the launcher's grants**: one
   identity, not "the app at the top of the process tree, which the hint names".
6. **In CI, the launcher is the one identity a hosted macOS runner allows**: its TCC rows and its
   screen-capture approval, in place of rows for each app under test and for bash. Writing them
   needs the owner's approval, as Snap's did.
7. **The project's build restarts itself under the launcher** (the owner's decision, 2026-11-09).
   Since ADR 0015, axx hands over twice before a command runs. The installed axx hands over to
   the version the project names, and that version hands over to the project's own build. Both
   hand-overs replace the process with `exec` (`internal/cli/packs.go`), so neither changes who
   macOS holds responsible. The project's build has desktop-macos, so it knows whether a command
   drives Mac apps, and an older installed axx hands over to it without knowing about the
   launcher. The core stays without purego.
8. **`AXX_MACOS_LAUNCHER=off` opts out** (the owner's decision, 2026-11-09, as `AXX_LAUNCHER=off`,
   renamed because `AXX_LAUNCHER` already tells a project's build the version of the installed axx
   that started it, ADR 0015), for someone who prefers their terminal's grants: axx then runs as
   it does without the launcher. `AXX_MACOS_LAUNCHER` may also name the launcher's `axx.app`.
9. **The launcher may ask for what the apps under it ask for.** An app under the launcher is
   its child, so macOS asks for the app's permissions on the launcher's behalf and judges each
   by the launcher's signature and Info.plist, not the app's. The launcher is signed with the
   entitlements the hardened runtime needs for some of them, those an IDE that runs programs has
   (IntelliJ IDEA's seven: Apple Events, the camera, the microphone, contacts, calendars,
   location and photos; `internal/tools/axx-launcher/entitlements.plist`), and its Info.plist has
   the message macOS shows for each permission an app may ask for. Without them, macOS refuses
   without asking anyone (the evidence of 2026-11-10, below).
10. **The release signs and notarizes the macOS `axx` binaries too** (the owner's decision,
   2026-11-09), with the same certificate and key. The Homebrew cask then stops removing macOS's
   quarantine flag (`.goreleaser.yaml`).

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

On 2026-11-09 they passed again on macOS 27.0.1 with `CGO_ENABLED=0`, so the function the
handoff uses is still there.

## Evidence: the signed launcher, 2026-11-09, on the owner's MacBook (macOS 27.0.1)

`scripts/macos-launcher-app.sh` built `axx.app`: a universal `axx-launcher`, identifier
`us.nimbusxr.axx.launcher`, signed with "Developer ID Application: Nimbus XR LLC" under the
hardened runtime. A program the launcher ran reported the process macOS held responsible for it,
and whether it could use the accessibility tree and capture the screen. macOS's privacy log
showed each request judged for `us.nimbusxr.axx.launcher`.

| Under | Charged to | Device Control and Data Access | Screen Recording |
|---|---|---|---|
| the launcher, before the owner allowed it | the launcher | no | no |
| the launcher, after one approval of each (a program and its child) | the launcher | yes | yes |
| the same launcher, a rebuilt program | the launcher | yes | yes |
| a newly signed launcher in another folder (another hash, as an update) | the launcher | yes | yes |
| an ad hoc signed copy with the same identifier | the copy | no | no |

The real axx agreed: `axx doctor` in the depot desk's acceptance project passed both macOS
checks under the launcher, and failed them under the ad hoc copy. So a grant is kept by NimbusXR's
signature, across rebuilds of what runs under it and across the launcher's own updates, and an
identifier alone gets nothing.

What it showed that the plan has to take in:

- **Screen Recording is not asked for the way Accessibility is.** On macOS 27,
  `CGRequestScreenCaptureAccess` under the launcher showed no prompt and listed nothing, and
  System Settings' + did not add the app. A real capture did: `screencapture` under the launcher
  was refused, and macOS's own alert listed "axx" in Screen & System Audio Recording, where the
  owner turned it on. Accessibility's prompt (`AXIsProcessTrustedWithOptions`) listed it in
  Device Control and Data Access, which is macOS 27's name for Accessibility.
- **`axx doctor` names the app at the top of the process tree**, IntelliJ IDEA in this test,
  where the launcher is the one macOS asks: decision 5's check, still to do.
- **The hardened runtime takes `DYLD_*` variables out of the environment** the launcher passes on.
  A variable set before axx starts does not reach axx under the launcher; the ones axx sets for
  an app it starts (models-macos's library) are set after it, and reach the app.

## Evidence: what the apps under the launcher ask for, 2026-11-10

axx 0.6.4's launcher had no entitlements and no messages. On the Mac mini, macOS's privacy log
showed it refusing Apple Events for the depot desks under it: "Prompting policy for hardened
runtime; service: kTCCServiceAppleEvents requires entitlement
com.apple.security.automation.apple-events but it is missing for
responsible=us.nimbusxr.axx.launcher". The depot desks use none of these permissions, so their
scenarios passed. Before 0.6.4, `open` made each app responsible for itself, and it asked with its
own signature.

On the owner's MacBook (macOS 27), a small program asked for the camera
(`AVCaptureDevice.requestAccess`) and to control Finder (`AEDeterminePermissionToAutomateTarget`,
asking the person if needed), under two launchers:

| Under | The camera | Controlling Finder |
|---|---|---|
| 0.6.4's launcher | refused at once; nothing shown | refused at once (-1743); nothing shown |
| a copy signed with the seven entitlements, with the messages, under an identifier of its own | macOS asked the owner | macOS asked the owner |

macOS's privacy log said why. For 0.6.4's launcher it said "Policy disallows prompt" for the camera, and
the missing entitlement for Apple Events, as on the Mac mini. For the copy it said "Prompting policy for
hardened runtime; allow prompt: Allow".

macOS's check that asks no one (`AEDeterminePermissionToAutomateTarget` without asking) answered
"not asked yet" under ad hoc signed copies with and without the entitlements alike: only a prompt
shows the difference, and no one answers one in CI. So the launcher's test
(`internal/tools/axx-launcher`) builds the app as a release does, with
`scripts/macos-launcher-app.sh`, and checks that it is signed with the entitlements and has a
message for each permission.

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
- [x] **The owner:** NimbusXR in the Apple Developer Program (2026-11-09).
- [x] **The owner:** a Developer ID Application certificate ("Developer ID Application: Nimbus XR
  LLC", team 2SAF8A2PAY), and an App Store Connect API key for notarization, which Apple's notary
  service accepted (2026-11-09). They are in the `release` environment: the variable
  `APPLE_TEAM_ID`, and the secrets `APPLE_DEVELOPER_ID_P12` (base64), its password
  `APPLE_DEVELOPER_ID_P12_PASSWORD`, `APPLE_API_KEY_P8`, `APPLE_API_KEY_ID` and
  `APPLE_API_ISSUER_ID`.
- [x] Build, sign and test `axx.app` on a Mac (`scripts/macos-launcher-app.sh`; the evidence above).
- [x] Release (`release.yml`): the `macos-launcher` job builds `axx.app`, signs it and sends it to
  Apple's notary service, and puts it on the release as `axx-launcher_<version>_darwin.zip`, with
  its build's provenance attested. GoReleaser signs the macOS `axx` binaries and sends them too
  (`.goreleaser.yaml`'s `notarize`, before they are archived and summed), and the Homebrew cask no
  longer removes the quarantine flag. The `notarized` job then waits for Apple's answer.
  - **Stapling, only when Apple is quick.** A CI job ends within 10 minutes, and Apple took 35
    minutes over NimbusXR's first submission (a second took under 30 seconds). The launcher's job
    waits 6 minutes at most, and staples the ticket when Apple has notarized it by then. A
    launcher left unstapled works the same: axx downloads it itself, so macOS never quarantines
    it and never asks Gatekeeper about it, and macOS keeps a person's grants by its signature.
    The macOS binaries cannot be stapled at all; Gatekeeper asks Apple about them online.
  - **Apple's answer in a job of its own** (`scripts/notarized.sh`): it fails when Apple has fewer
    submissions than the release sends (three: the launcher and the two macOS binaries), or
    refused any, with Apple's log of it, and warns when Apple is still at it after 8 minutes.
  - **A check before any release**: the `macOS signing` workflow, run by hand from main (the
    release environment's secrets are main's only), signs and notarizes the launcher and the
    macOS binaries as a release does, and publishes nothing. Its first run (2026-11-10, run
    42751460142) signed all three, and Apple accepted them: the launcher in under 30 seconds,
    stapled and passed by Gatekeeper ("Notarized Developer ID"), and GoReleaser's two binaries.
    Its `notarized` job failed: the signing action imported a certificate in every job on a Mac,
    and that job passes none. It now imports one only when a job passes it.
- [x] axx downloads it on first use, checks its signature and team, and keeps it in its cache
  (`<user cache>/axx/launcher/axx.app`): the launcher of the running axx's release, through
  `AXX_DOWNLOAD_URL`'s mirror when one is set, installed once `codesign` says NimbusXR signed it.
  An axx built from source downloads none; a release from before the launcher is asked once; a
  download that fails is tried again an hour later. `AXX_MACOS_LAUNCHER` may name another
  `axx.app`, checked the same way.
- [x] On macOS, `axx run`, `axx mcp` and `axx doctor` run under it (`core.Relauncher`, which
  desktop-macos implements): once, in the outermost process. The run Delve starts for
  `--debug-steps` knows it is under the launcher (`AXX_UNDER_MACOS_LAUNCHER`), and does not run again.
  `AXX_MACOS_LAUNCHER=off` runs without it.
- [x] Under it, desktop-macos starts apps from their bundle's executable, with the arguments
  `open` gives them and the environment it does (the user's session's, and what the
  registration and packs add). Apple's apps, and an app asked to open documents, which only
  LaunchServices hands it, still open with `open`. Without the launcher, every app opens as before.
- [x] `axx doctor` and the first desktop step check the launcher's grants, and name it, and have
  macOS list "axx" for the person to turn on: Accessibility's prompt, and for Screen Recording one
  real capture.
- [x] The Mac mini: the launcher in `runner`'s axx cache, allowed in both settings (2026-11-09).
- [x] The Mac mini's desktop jobs pass under it (2026-11-10, run 42734973419): `axx doctor`
  reports the launcher and its two permissions, and the AppKit and SwiftUI desks, started as
  axx's children, pass every scenario. AppKit's "An arrival shows how it was registered" failed
  once before a rerun passed; a project story follows it.
- [x] The launcher may ask for the permissions the apps under it ask for (decision 9): its
  entitlements and messages, and a test that builds the app and checks them (2026-11-10). 0.6.4's
  launcher has neither, so under it macOS refuses those permissions without asking;
  `AXX_MACOS_LAUNCHER=off` avoids that until a release has the fix.
- [ ] CI: the hosted macOS recipe (the launcher's TCC rows and approval), with the owner's
  approval. Snap's CI then grants the launcher instead of Snap and bash.
- [x] Docs: the desktop guide's permissions (`guides/test-desktop-apps.md`) and desktop-macos's
  reference.
- [ ] ADR 0011's checks and CI, once the launcher is in a release.

## Open questions

1. **The private API.** If a macOS release removes it, `Start` fails as the library loads, and axx
   runs as it does today, with the host's grants; `axx doctor` says so.

The name and identifier, where axx restarts itself, and the opt-out were open here until the
owner decided them on 2026-11-09 (decisions 4, 7 and 8).

## Consequences

- One grant per Mac, kept across axx's updates, that works from any terminal, IDE or CI agent.
- The apps under test share it, as axx's children, and so do the desktop steps an agent tries
  through `axx mcp`.
- CI allows one identity. There are no rows per app under test, and no bash alert.
- axx's release signs and notarizes a macOS app under NimbusXR's Apple account.
- axx relies on a private macOS API that Chromium and VS Code also rely on, and falls back to
  today's behaviour without it.
- Apps start from their executable rather than through LaunchServices. Snap ran that way in the
  prototype; an app that needs LaunchServices (one that expects to be opened by its bundle) will
  show which.
- `DYLD_*` variables set before axx starts do not reach it under the launcher (the hardened
  runtime's), nor the apps it starts unless axx sets them itself.
