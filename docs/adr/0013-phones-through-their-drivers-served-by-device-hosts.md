# 0013: Phones are driven through their own drivers by axx, and served by device hosts

- Status: Proposed
- Date: 2026-10-10

## Context

The mobile packs drive apps through Appium: axx downloads Node.js and Appium with one driver
per platform (`appium-3.8.0-xcuitest-12.13.3`, `appium-3.8.0-uiautomator2-8.7.0`), starts an
Appium server per device, and sends it WebDriver commands. Appium forwards most of them to a
driver on the device: WebDriverAgent (an XCTest runner) on iOS, the UiAutomator2 server (an
instrumentation) on Android. Those two read the accessibility tree, tap, type and stream the
screen. Around them, Appium's drivers install apps, grant permissions, set the locale and the
location, open links, move files and start each session.

What that costs, measured:

- **Session start.** On a private repository's CI, creating a session took 70 s, then 22 s
  and 14 s for each later scenario of DTCD's Android suite: the driver's ritual for every
  session (checks, helper apps, settings), on top of the app's install.
- **Node.js.** axx downloads it with Appium on first use; the server's start takes seconds,
  and over a minute on a busy CI machine.
- **A hop and a ritual we already route around.** axx launches WebDriverAgent on each
  simulator itself, because the driver quit and relaunched it for every session (10 to 30 s a
  scenario), and installs Appium's prebuilt WebDriverAgent instead of letting the driver build
  it with Xcode. On iOS, Appium is already mostly a proxy.

What Appium does not cost: the problems DTCD's suite met were in the drivers on the device
(WebDriverAgent's "max scroll count", UiAutomator2's wait for the root accessibility node,
Compose reporting nothing more to scroll). Leaving the Appium server does not remove them;
owning the code that talks to the drivers lets axx work around them where they are.

The owner's direction (2026-10-10): drop the Appium server, and plan for a hosted platform,
since many teams will want to run axx without the compute, while anyone can still host devices
anywhere, real ones included.

### Options considered

1. **Keep Appium.** Nothing to build; its costs stay, and real devices stay a device farm's
   (Appium is the protocol farms speak).
2. **Drive the drivers directly, from Go.** Keep WebDriverAgent and the UiAutomator2 server,
   pinned; do what Appium's drivers did around them in axx. No Node.js, sessions that last
   the device's lease, one hop.
3. **Write our own drivers** (an XCTest runner, an instrumentation). Full control, and years
   of XCTest and UiAutomator quirks to learn again, on every OS release. Not now: where a
   driver blocks a scenario, a native helper next to it (as the AR emulation spike injects a
   library into the app) does the one thing.
4. **Real devices through farms.** Keep an Appium client for them and stop there. It leaves
   real devices to someone else's protocol and pricing, and no way to host one's own.

## Decision

Option 2, with the device layer built around a host boundary, so that the same code drives a
device in the axx process and serves it to runs elsewhere.

### The drivers

- **iOS:** WebDriverAgent, pinned with its SHA-256, as now. Simulators install the published
  simulator build (`WebDriverAgentRunner-Build-Sim-<arch>.zip`) with `simctl`. A real iPhone
  needs it signed by the team that owns the device: built and signed once on a Mac, by an axx
  command, and kept on the host.
- **Android:** the UiAutomator2 server's two published APKs, pinned with their sums,
  installed once per device and started as an instrumentation; axx restarts it when it dies.
- axx speaks their HTTP protocols (WebDriver with their own endpoints) through the client it
  has, without Appium's `/session` ritual: a driver starts with the device's lease and stays up
  until the lease ends; a scenario's reset is the app's (stop, clear, reinstall when asked).

### What axx does that Appium's drivers did

| | iOS simulators | Android emulators and devices |
|---|---|---|
| Install, launch, stop, clear an app | `simctl` | `adb` (package manager, `am`) |
| Permissions, locale, time zone, location | `simctl privacy`, defaults, `simctl location` | `pm grant`, settings, `emu geo fix` |
| Deep links | `simctl openurl` | `am start -d` |
| Files in the app's sandbox | `simctl get_app_container` | `run-as`, push and pull |
| Screen, source, taps, typing, gestures, alerts | WebDriverAgent | UiAutomator2 server |
| Screen stream (traces and videos) | WebDriverAgent's MJPEG | UiAutomator2's MJPEG |
| Notifications | WebDriverAgent on SpringBoard | the shade, through UiAutomator2 |

Real iPhones use [go-ios](https://github.com/danielpaulus/go-ios) (MIT, Go, no CGO): the
phone over USB, its developer disk image, iOS 17's tunnel, app installs, starting
WebDriverAgent's XCTest runner and forwarding its ports. Android's are `adb`, as emulators.
The `CGO_ENABLED=0` rule holds.

### The device layer and its host

`mobile-core`'s `Device` is the boundary: what a scenario does with a device (read the
screen, find a control by name and tap it, fill, scroll to show, swipe, open a link, files,
the screen stream, reset). Two implementations:

- **In the axx process**: the drivers above, on the machine's simulators, emulators and
  attached devices. This replaces Appium for every local run.
- **A device host**: an axx process, on any machine with devices, that serves them over a
  protocol axx owns (HTTPS and WebSocket). A run reaches it by a property of the app's
  registration, as it reaches an Appium server today; leases and the per-scenario resets
  happen in the host, so a scenario's isolation is the same wherever its device is
  ([isolation](0012-one-vocabulary-for-apps-and-a-file-context-for-everything-under-test.md)).

The protocol is axx's actions, not WebDriver's commands: "find the Sign in button and tap it"
is one call, resolved next to the device, not six round trips across a network. That is where
a remote run's speed comes from, and what a hosted platform runs on: hosts, a scheduler that
leases their devices, and storage for traces and videos.

Device farms, which speak Appium's protocol, keep working through the `appium` property: the
WebDriver client axx has, without a local Appium server.

## Plan

Each step ends with the parcels example and DTCD's suite passing, locally and in CI, and with
the measurements beside the ones before it.

1. **Android, directly.** The UiAutomator2 server installed and started by axx; the Android
   pack on it; measured on DTCD's Android suite: session start, time per step, first run.
2. **iOS simulators, directly.** WebDriverAgent as now, without the XCUITest driver.
3. **Appium out.** No Node.js download; the `appium` property stays, for farms.
4. **The device host**: serving a machine's simulators and emulators to runs elsewhere, with
   leases, resets, the screen stream and files.
5. **Real Android devices** through a host.
6. **Real iPhones** through a host: go-ios, and WebDriverAgent signed by the owner's team.
7. **The platform**: its own decision record.

## Consequences

- No Node.js, and a first run that downloads two drivers, not a package tree.
- A scenario's session starts in the time its app takes to reset, not the driver's ritual.
- axx owns the code around the drivers, and the breakage new OS releases bring to it.
- Traces and videos read the drivers' screen streams, so they carry over unchanged.
- Real devices become something anyone can host, and the platform hosts many.

## Open questions

- The host's command and the registration property that names a host.
- Whether the host's protocol is JSON over HTTPS and WebSocket, or gRPC.
- How a team signs WebDriverAgent for its iPhones: an axx command that runs `xcodebuild` with
  its team, or a signed build it brings.
