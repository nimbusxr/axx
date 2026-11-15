# 0019: Devices and desktop apps start in the axx host, each app once its person approved it

- Status: Accepted
- Date: 2026-11-14

## Context

ADR 0018 gave a pack a side in the axx host, and web-core's starts the browser a sandbox keeps
from starting. It left devices and desktop apps for later: "what they give a run is different,
an app the agent built that runs on the device or the machine, and each needs its own answer to
decision 2 before it is built."

They hit the same wall. In an agent's sandbox on macOS, `simctl` cannot reach CoreSimulator's
service, so no iOS simulator boots. An Android emulator needs the machine's hypervisor. A desktop
app needs the window server and the accessibility API. A run there cannot start a phone or an
app, and nothing in a project fixes that.

A browser could be given to a run with nothing else of the machine: its network goes through
the run, and its commands are checked one by one. An app cannot. It is a program the agent
writes:

- **In the iOS Simulator** it is a process of the Mac, with the person's files and network.
- **As a Mac, Windows or Linux app** it is the same.
- **On an Android emulator** it runs in a virtual machine and reads no file of the machine, but
  its network is the machine's. The emulator's proxy (`-http-proxy`) carries TCP only, and its
  DNS goes out directly, so its network cannot be sent through the run as a browser's is.

So decision 2 of ADR 0018 ("a pack's host side gives a run only what its sandbox would let it
have") cannot hold for the app itself. It can hold for everything else.

## Decision

**1. The person approves each app before the host starts it.** A pack's side asks through
`core.HostRun.Approve`, with a subject ("the courier app, in the iOS Simulator"), a text and
warnings. The text is everything of the app's registration that decides what runs: the app, its
arguments and environment, the device, the files it puts on the device, the settings of the
person's that it empties. The warnings say what that kind of app reaches. The host asks in its
terminal or in the editor that started it, never over its port, keeps the answer in the
person's configuration directory, and asks again when the text changes, with the difference.
Nothing of the app is reset, installed or started before the answer. This is so on every
platform, Android included: its network is not held to the sandbox.

**2. The approval is for the registration, not for one build.** The agent builds the app again
and the next run starts what it built: that is what the tests are for. The question says so. A
person who must hold the agent's code to the sandbox does not approve apps.

**3. Beyond the app, a run gets only what a step does.** What crosses to the host is the
boundary the packs already had between "what a step wants" and "the machine":

- **Phones:** `mobilecore.Platform` and `Device`. The run asks to start an app, launch it, read
  its screen, open its notifications, and so on. It drives the screen itself, through the
  device's driver (WebDriverAgent, the UiAutomator2 server), which listens on loopback and does
  only what a driver does.
- **Desktops:** `desktopcore.Driver`, `Desktop` and `Process`. The run asks to claim the
  desktop, start an app, find a control, click it, type, read a window.

Each is an operation with a name and checked parameters (`packs/internal/hostwire`). A run names
no program and passes no argument to one. The host registers the app itself, from the rows the
run's step read, in a scenario of its own (`packs/internal/hostscenario`), and runs the pack's
own code there: its device pool, its installs, its resets, its accessibility tree.

**4. The files a run names are the project's.** The host's scenario is in a suite confined to
the project's repository (`core.Suite.Confine`): the nearest folder at or above `axx.yaml`'s
that has a `.git`, else that folder. The host settles it as it starts. A file is judged after
its links are followed, and one outside is refused. A desktop app may also be one the OS finds
by name, which the approval shows.

**5. The packs that build on a platform pack work in the host's scenario.** The model stubs
prepare an app as it launches, where it launches. A hook of the phase `core.HostScenario` sets
them up in the host's scenario as their step hook does in a run's. What the two sides must know
of each other they note of the scenario (`core.Scenario.Note`), and the notes cross with the
calls: that the scenario's steps use the microphone, which files the scenario wrote for the app.

**6. A run that a person watches or pauses starts its phones itself,** as it starts its
browsers: the windows are theirs, outside a sandbox. Without a host, nothing changes.

**7. On a Mac, `axx host` runs under axx's launcher** (ADR 0016), as a run does: macOS asks the
launcher for the permissions to drive apps.

## Consequences

- With `axx host` running, mobile and desktop scenarios run from an agent's sandbox on every
  platform pack, with nothing in `axx.yaml` and no change to a feature. The person answers one
  question for each app.
- The sandbox's network rules do not apply to an approved app. The guide says so, by platform.
- The check of a file's place and its read are two moments. An agent that swaps a file for a
  link between them could have the host read a file outside the repository, and put it on a
  device as an app's photo or document. The guide says so.
- A pack that builds on a platform pack and keeps state in a scenario must decide which side
  has it. A launch hook needs a `HostScenario` hook too, and what the other side needs is a
  note.
- The host and its runs must be the same axx.
- The platform packs' tests run a second time through an axx host in the tests' process
  (`AXX_TEST_HOSTED`), and CI runs devices' tests that way in parts of their own.

## Alternatives

- **Send an app's network through the run, as a browser's.** An emulator's proxy carries TCP
  only, and its DNS goes out directly. A simulator's and a desktop's apps use the machine's
  network with no proxy axx gives them. A user-mode network stack of axx's own for the emulator
  (`-net-socket`) would hold Android only, and is a large thing to keep.
- **Hand the run the device tools** (`simctl`, `adb`, the emulator) through the host. Each is a
  way to run a program or read a file on the machine. The run would have the machine.
- **Approve nothing, as for a browser.** An app is the agent's code outside the sandbox: a
  person must know, and say yes.
- **Approve each build** (a hash of the app). The agent builds on every change, so the person
  would answer on every run, and would learn to answer without reading.
- **Loosen the sandbox.** Not ours to ask (ADR 0018).
