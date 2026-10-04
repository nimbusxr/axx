# 0011: Desktop apps are tested through the operating system's accessibility tree

- Status: Proposed
- Date: 2026-10-04

## Context

axx tests web apps in real browsers and mobile apps on emulators and simulators. Desktop apps
are the gap: apps on Windows, macOS and Linux, native or built on a webview (Tauri, Wails,
Electron, CEF, Qt, Flutter). Snap, a Tauri app, is the first project that needs it.

Every desktop framework comes down to one of three operating systems and, for its webview, one
of three web engines: Chromium (WebView2 on Windows, Electron, CEF), WebKitGTK (Tauri and Wails
on Linux) and WKWebView (Tauri and Wails on macOS). Frameworks add no cases of their own.

axx tests black-box: it starts what it tests as it ships, and reaches it the way its users or
other systems do. Testing a desktop app must not need a build of the app wired for testing.

### Options considered

1. **Each framework's test driver** (`tauri-driver`, Electron through Playwright, and so on).
   This is code for every framework, unevenly supported: `tauri-driver` has no macOS support,
   which is why Snap added a WebDriver plugin under an `acceptance` feature. A wired build.
2. **Each web engine's inspector**: the Chromium DevTools protocol (a launch switch), the
   WebKitGTK inspector server (an environment variable) and, for WKWebView, an agent loaded
   into the app at launch. The agent worked on a plain Snap build (it listed the webview, read
   and clicked the toolbar, ran scripts), but macOS loads it only into an app signed to allow
   it: a wired build again, for one of the three engines. It reaches webviews only, not native
   windows, menus or system dialogs.
3. **Appium's desktop drivers**: mac2 on macOS (needs full Xcode and builds its own
   WebDriverAgent), NovaWindows on Windows (a community driver over PowerShell), nothing on
   Linux. Three stacks of uneven quality, Node underneath, and a third of it to write anyway.
4. **The operating system's accessibility tree, through axx's own drivers in Go**: UI
   Automation on Windows, AX on macOS, AT-SPI on Linux.

## Decision

Option 4. Desktop apps are tested through the accessibility tree, by drivers axx writes in Go
for the three operating systems.

- **Black-box.** The accessibility tree is public on every app as it ships: it is what screen
  readers read. All three web engines publish their pages to it (buttons, fields and text by
  the names people see), so it covers native and webview content alike, with no app change.
  macOS asks that the process running axx be allowed to control the computer (Accessibility,
  and Screen Recording for screenshots): a setting of the machine, never of the app.
- **One model on every OS.** Controls are found by role and name, as in the mobile packs, with
  identifiers (`id=`) and XPath over the tree (`xpath=`) as fallbacks. Content drawn on a
  canvas is checked by screenshots.
- **Pure Go, no cgo**, so `CGO_ENABLED=0` builds keep working: AX through purego, UI
  Automation through COM over syscalls (go-ole), AT-SPI over D-Bus (godbus), X11 input through
  a pure-Go X client. These dependencies live in the pack, never in the core binary (ADR 0009).
- **No engine protocols, no injection.** Running scripts in a page and reading an app's
  internal state are white-box, and outside what axx tests.

### What it gives up

- Running JavaScript in webviews and reading an app's internal state.
- `css=` and `testid=` selectors in webviews. A DOM element's `id` does reach the tree in all
  three engines (AXDOMIdentifier, AutomationId, an AT-SPI attribute); Phase 0 checks it.
- Controls with no accessible name cannot be found by name, only by `id=` or `xpath=`. That is
  an accessibility bug of the app, which the tests surface.
- Speed: accessibility calls are slower than an engine protocol. The drivers query a window's
  tree, not the whole desktop, and wait as the mobile packs do.

## The pack

To be settled in Phase 2, with the step text approved before it is built.

- **One pack, `desktop`, with a driver for each OS**, chosen by the OS axx runs on. Unlike
  mobile, where one Mac runs both Android and iOS, the operating system is the machine's, not
  the scenario's: a feature runs unchanged on each OS a project tests on.
- **Registration in a step**, with how to start the app on each OS, like the mobile packs'
  registrations:

  ```gherkin
  Given the snap desktop app with the following properties:
    | macos   | ../app/src-tauri/target/release/bundle/macos/snap.app |
    | windows | ../app/src-tauri/target/release/snap.exe              |
    | linux   | ../app/src-tauri/target/release/snap                  |
    | args    | --overlay-mode                                        |
  ```

- **Wording**: the mobile packs' conventions in the desktop's own words, "clicked" rather than
  "tapped". `the {word} app shows {string}` is mobile-core's already, and a project may use
  both packs, so desktop steps need their own noun. "Window" reads naturally on a desktop:

  ```gherkin
  When the "Rectangle" button is clicked in the snap window
  Then the snap window shows "Rectangle selected"
  ```

  Candidates only, for approval.

### Isolation

Every scenario starts from a clean app, with no opt-out, as in the mobile packs:

- axx starts the app for the scenario and stops every instance of it afterwards, its child
  processes too.
- Each scenario gets a home of its own: on macOS and Linux, `HOME` and the XDG directories; on
  Windows, `USERPROFILE`, `APPDATA` and `LOCALAPPDATA`. Apps keep their data under these, so
  each starts empty, without the app knowing about tests.
- What a home does not reach (the macOS keychain, the Windows registry, system-wide state) is
  named in the docs, and the registration can name what to clear.
- One desktop session has one screen and one keyboard focus, so desktop scenarios run one at a
  time under a desktop lease, like a mobile device lease. On Linux, a virtual display and
  accessibility bus for each worker can let them run at once.

### Input and screenshots

- Accessibility actions first: press, set a value, focus, select. They need neither the
  pointer nor focus.
- Real keyboard and pointer input where an action cannot do it (drawing on a canvas, a drag):
  CGEvent on macOS, SendInput on Windows, XTest on X11. Wayland restricts synthetic input, so
  Linux runs under X11 or Xwayland at first.
- Screenshots of a window, compared per platform like the web pack's.

### Checks before a run

`axx doctor` and the first desktop step report a missing Accessibility or Screen Recording
permission on macOS, a missing accessibility bus on Linux, and a missing interactive session
on Windows, each with how to fix it.

## Plan

Each phase ends with what it proved. Snap is the proving ground, and the parcels example is
where the steps are modelled.

0. **Prove AX on macOS, in pure Go, on Snap as built for release.** This is the riskiest
   driver. Check that the process is trusted, find Snap's window, read the toolbar's tools and
   colors by name, press one, read the change, check that DOM ids appear as identifiers, and
   time a tree read. No cgo and no change to Snap.
   *Proven on 2026-10-04 (macOS 27.0.1)*, on Snap's release build (`tauri build`, no
   `acceptance` feature, ad-hoc signed), by `packs/desktop/internal/ax` with `CGO_ENABLED=0`:
   - the page's tree came in 575ms after the window, with nothing set on the app;
   - buttons were found in 1 to 6ms by the names people see: an HTML `title` reaches AX as the
     tooltip (`AXHelp`, "Rectangle (R)") on a button with text, and as its description
     ("Blue") on one without;
   - `AXPress` selected the rectangle tool, the blue color and the thick width, and the
     app's state changed (`AXDOMClassList` went from `tool-btn` to `tool-btn active`);
   - DOM ids reach AX as `AXDOMIdentifier` (`btn-save`), so `id=` works in webviews;
   - the window's whole tree (28 elements) reads in about 25ms.

   Two findings for later phases: on macOS 27, the permission is under Privacy & Security >
   Device Control & Data Access, granted to the app at the top of the process tree (here the
   IDE whose terminal ran the tests); and Snap marks its selected tool with a CSS class only,
   which no accessibility API reports across platforms (`aria-pressed` would be).
1. **Prove AT-SPI and UI Automation.** AT-SPI over D-Bus on Snap's Linux build under a virtual
   display (WebKitGTK). UI Automation over COM on Snap's Windows build on a hosted Windows
   runner (WebView2). Settle what CI needs on each OS: whether hosted macOS runners can grant
   Accessibility, whether hosted Windows runners have an interactive session, and the Linux
   display and accessibility bus.
   *Proven on 2026-10-04*, on Snap built for each OS as its release would be (`tauri build
   --no-bundle`), unchanged, with `CGO_ENABLED=0`:
   - **Windows 11** (WebView2), by `packs/desktop/internal/uia` (UI Automation's COM
     interfaces through their vtables): the app's window came in 1.3s after a hidden one of
     Tao's, its page's buttons 0.3s later; the whole toolbar reads in 1ms. A `title` reaches
     UI Automation as help text and description, a button without text is named by it, a DOM
     id is the AutomationId and DOM classes are the ClassName. Invoke selected the rectangle
     tool, the blue color and the thick width (`tool-btn` became `tool-btn active`).
   - **Linux** (WebKitGTK 2.50 on Xvfb, Debian 12), by `packs/desktop/internal/atspi` (AT-SPI
     over D-Bus): the app came on the accessibility bus in 275ms and its buttons 17ms later;
     the tree (28 elements) reads in 82ms. A `title` is the description, a DOM id the `id`
     attribute; the press action pressed each button. WebKitGTK does not give DOM classes,
     so on Linux an app's state shows only through ARIA states and text.

   What the drivers had to learn:
   - Windows: a program started over SSH runs in a session without a desktop; one started by
     a scheduled task for the signed-in user (`schtasks /it`) runs on the desktop.
   - Windows: Chromium builds a page's tree once a client reads its document.
   - Linux: a child in another process (WebKitGTK's page under its web view) comes only by
     index (`GetChildAtIndex`), not from `GetChildren`; WebKitGTK answers `GetRoleName` with
     nothing and never answers `GetActions`, so the driver reads the role's number and the
     actions one by one; every call has a timeout, as an app may not answer.
   - Linux: the session needs a D-Bus session bus and at-spi2-core's accessibility bus;
     WebKitGTK's sandbox needs user namespaces, which a container does not give.

   Still open for CI: whether hosted macOS runners can grant Accessibility, and whether
   hosted Windows runners give a desktop session.
2. **Design**: the pack's shape, the registration, the step text (approved before building),
   the isolation and the lease. This ADR is then accepted or amended.
3. **Build** the desktop pack: the three drivers behind one interface, locating, waiting,
   actions, input, screenshots, isolation, the lease, and doctor's checks.
4. **Model and document**: a parcels desktop app in `examples/parcels` with a scenario for every
   step, a guide, the generated reference, the skills, and a CI job on each OS.
5. **Snap moves to it**: its features use the desktop steps, and the `acceptance` feature and
   its WebDriver plugin go.

## Consequences

- axx tests desktop apps on all three operating systems the way it tests mobile apps, with no
  build wired for testing and no code for any framework.
- axx owns three accessibility drivers. Their surface is small (read the tree, act, type,
  capture), but each OS has its own edge cases.
- Hosted CI for macOS depends on granting Accessibility there, which Phase 1 settles.
- Wayland-only Linux desktops are out of reach until synthetic input works there.
