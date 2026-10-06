# 0011: Desktop apps are tested through the operating system's accessibility tree

- Status: Accepted; amended by [0012](0012-one-vocabulary-for-apps-and-a-file-context-for-everything-under-test.md)
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
- **Pure Go, no cgo**, so `CGO_ENABLED=0` builds keep working: AX and CGEvent input through
  purego, UI Automation through COM over syscalls (go-ole) and the Java Access Bridge's DLL
  for Java on Windows, SendInput, AT-SPI over D-Bus (godbus), and XTest input through a pure-Go
  X client (xgb). These dependencies live in the pack, never in the core binary (ADR 0009).
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

## The packs

*Designed in Phase 2, and accepted with its step text on 2026-10-04. Amended by ADR [0012](0012-one-vocabulary-for-apps-and-a-file-context-for-everything-under-test.md):
the steps move to `app-core` and say "the {word} app"; an app is registered once per OS
(`the depot macos app with…`); a desktop app's home is no longer a value of the scenario (an
app's files are reached through file contexts); and each Linux desktop has its own file
system. Where this section says otherwise, 0012 decides.*

### Shape

- **Four packs, as mobile has them.** `desktop-core` has what every OS shares: the steps and
  `{desktopControl}`, the registration, finding controls, waiting, comparing screenshots, and
  the app's home. `desktop-macos`, `desktop-windows` and `desktop-linux` each have their OS's
  driver: its accessibility API (AX; UI Automation and the Java Access Bridge; AT-SPI), its
  input, launching, its part of the reset, capturing windows, the switches its frameworks
  need, and `axx doctor`'s checks.
- A project adds the packs of the OSes it tests on (`axx pack add desktop-macos` brings
  `desktop-core` with it), and a feature runs unchanged on each. A run on an OS whose pack the
  project does not have fails its first desktop step, naming the pack to add.
- In the catalog: `desktop-core`, "desktop apps, used as people use them: launched, their
  controls clicked and filled, what they show checked"; `desktop-macos`, "macOS apps, through
  AX: a clean app for each scenario"; `desktop-windows`, "Windows apps, through UI Automation
  and the Java Access Bridge: a clean app for each scenario"; `desktop-linux`, "Linux apps,
  through AT-SPI, in desktops of axx's own: a clean app for each scenario".
- In `axx.yaml`: `packs.desktop-core.screenshots` sets the screenshots' `folder`, `tolerance`,
  `update` and `platforms`, as the web and mobile packs' screenshot steps have them;
  `packs.desktop-linux.desktops` sets how many Linux desktops run at once.

### Registration

```gherkin
Given the snap desktop app with the following properties:
  | macos   | ../app/src-tauri/target/release/bundle/macos/snap.app |
  | windows | ../app/src-tauri/target/release/snap.exe              |
  | linux   | ../app/src-tauri/target/release/snap                  |
  | args    | --overlay-mode                                        |
```

| Property | What it is |
| --- | --- |
| `macos` | The app on macOS: a `.app`, an installed app's bundle identifier (`com.apple.TextEdit`), or an executable. |
| `windows` | The app on Windows: an `.exe`, or a packaged app's ID (`Microsoft.WindowsCalculator_8wekyb3d8bbwe!App`). |
| `linux` | The app on Linux: an executable (an AppImage too), or a command on `PATH`. |
| `args` | The arguments it starts with, on every OS. |
| `env.<name>` | An environment variable it starts with, like `env.PARCELS_API`. |
| `locale`, `timezone` | The language and region (`de-DE`) and the time zone (`Europe/Berlin`) it starts in. macOS and Linux set them for the app; Windows has no language or time zone of an app's own, so there the machine's apply, and the run warns. |
| `registry` (Windows) | The keys under `HKEY_CURRENT_USER` the app keeps its settings in, like `Software\NimbusXR\Snap`: reset before each scenario (below). |

A run on an OS the registration names no app for fails its first step, naming the row to add.
The app starts when a step launches it. Every step names the app, so a scenario can drive two,
or a desktop app and a web app.

### Steps

Approved on 2026-10-04. The mobile packs' steps in the desktop's words: "clicked" where mobile taps. Mobile-core has
`the {word} app shows {string}`, and a project may use both packs, so the desktop's steps say
"desktop app" throughout: one noun keeps them uniform and easy to guess. Checks take
`within {duration}` (10 seconds without it), as mobile's do.

```gherkin
# The app
Given the {word} desktop app with the following properties:
When the {word} desktop app is launched
When the {word} desktop app is restarted

# Clicking, filling and keys
When the {string} {desktopControl} is clicked in the {word} desktop app
When the {string} field in the {word} desktop app is filled with {string}
When the {string} {desktopControl} is scrolled into view in the {word} desktop app
When the {word} key is pressed in the {word} desktop app

# Positions, for what an app draws rather than names (a canvas, a map)
When the {string} {desktopControl} in the {word} desktop app is clicked at {int}, {int}
When the pointer is dragged from {int}, {int} to {int}, {int} on the {string} {desktopControl} in the {word} desktop app

# Checks, each also as "within {duration} ..."
Then the {word} desktop app shows {string}
Then the {word} desktop app does not show {string}
Then the {string} {desktopControl} is shown in the {word} desktop app
Then the {string} {desktopControl} is enabled in the {word} desktop app
Then the {string} {desktopControl} is disabled in the {word} desktop app
Then the {string} field in the {word} desktop app has the value {string}
Then the {word} desktop app looks like the {string} screenshot
```

- `{desktopControl}` (parameter type names are global, and mobile-core has `{control}`):
  `button`, `field`, `checkbox`, `radio button`, `switch`, `tab`, `menu`, `menu item`,
  `list item`, `row`, `link`, `image`, `text` and `element` (anything), and their plurals. A
  menu command takes two clicks, as a person makes them: the "File" menu, then the
  "Export…" menu item.
- Keys have the web pack's names: `Enter`, `Escape`, `Tab`, `Control+Shift+S`, and
  `ControlOrMeta+Z`, which is Command on macOS and Control elsewhere, so a feature runs
  unchanged on each OS.
- Positions are points from the top left of the named control.
- `restarted` quits the app and launches it again, keeping what it stored; the next
  scenario's reset does not keep it.

In a scenario, with the depot desk the parcels example will have:

```gherkin
Scenario: A depot clerk registers an arriving parcel
  When the depot desktop app is launched
  And the "Reference" field in the depot desktop app is filled with "PX-DSK-4102"
  And the "Register" button is clicked in the depot desktop app
  Then the depot desktop app shows "Registered PX-DSK-4102"
  And the "Register" button is disabled in the depot desktop app
```

Not in this list, and suggested only for when a project needs them:

- opening a file in the app (`the {word} desktop app is opened with the {filepath} file`),
  for document apps;
- closing the app, and checking that it closed (closing that asks to save; Snap's overlay
  closing after a save);
- double-clicks, right-clicks (context menus) and dragging one control onto another, a
  checkbox's tick, an option chosen in a field, and table rows, as web-core has them;
- windows by their title, notifications, and the clipboard.

### Finding controls

- By kind and the name people see, exactly, as in the mobile packs. A control's name is its
  accessible name: its text or label, else its description, else its tooltip (a webview's
  `title` reaches AX as the tooltip of a button with text, and as the description of one
  without). Each kind is the roles each OS gives it; the reference lists them.
- `id=` finds a control by its identifier: AXIdentifier, or AXDOMIdentifier in webviews, on
  macOS; the AutomationId on Windows; the `id` attribute on Linux. `xpath=` finds it in the
  tree, its roles as the element names.
- A step looks in the app's windows, the front one first, with their dialogs, sheets and
  menus, and on macOS the app's menu bar. The app's windows are those of its process and the
  processes it started; on Windows, a packaged app's frame too.
- A failed step attaches a screenshot of the app's window and an outline of its tree (roles,
  names, identifiers), and names the controls of that kind it found: the names a person or an
  agent may have meant.

### Acting as a person does

Phase 1 found that apps ignore the accessibility equivalents of clicks and typing, or take them
and do nothing, so the steps use the pointer and the keyboard:

- **Clicked**: the app comes to the front, the control scrolls into view, and the pointer
  clicks its middle (CGEvent, SendInput, XTest). The control's accessibility action (AXPress,
  Invoke, AT-SPI's click) is the fallback for a control with no place on the screen (Flutter on
  Linux reports none), and the log says it was used.
- **Scrolled into view**, as a person scrolls: the app is asked first (AXScrollToVisible,
  ScrollItem, AT-SPI's ScrollTo), and where it cannot, each area the control is in scrolls in
  turn, the outermost first (the page, then the table on it), until the next one in shows. An
  area scrolls when asked (UI Automation's Scroll pattern), or under the wheel, turned over a
  part of it that no list or table covers, as that one would take the turns. A control is in
  view when its middle is in every area it is in, in its window and in the screen's usable part,
  and a hit test there finds it.
- **A window that does not fit** the screen's usable part (past its edge, under the taskbar or
  the Dock) is moved into it as the app launches, and made smaller if it is the bigger, as a
  person would; the app's content scrolls. On Linux the screen is axx's own (Xvfb), sized for
  the app.
- **Filled**: the field takes the focus, its text is selected and typed over with the keyboard,
  and its value is read back: a field that does not then hold the value fails the step. axx
  never sets a value through accessibility instead, since apps report values they never got
  (Flutter on macOS). Any character can be typed: as Unicode on macOS and Windows, and through
  a spare key mapped to it on Linux.
- **Keys** go to the app's focused window. **A drag** presses at the start, moves there in
  steps, and releases at the end.
- Actions wait for their control to be shown and enabled, 10 seconds. `${env:..}` values are
  secrets: typed, and masked in logs and failures.

### Each scenario has the desktop, and a clean app

**The desktop.** A desktop has one screen, one pointer and one keyboard focus, so desktop
scenarios take it in turns, while other scenarios run alongside them:

- On macOS and Windows, the desktop is the machine's, and two runs on one machine take turns
  too. While desktop scenarios run, the screen, the pointer and the keyboard are theirs: leave
  the machine be, or run them in CI or a virtual machine. The run is always on screen;
  `--slowdown` slows it down, as with browsers.
- Two copies of one app cannot run side by side and stay apart on macOS and Windows either:
  an app's preferences (macOS) and registry keys (Windows) are the user's, not the copy's. So
  desktop scenarios there go faster on more machines: running a run's desktop scenarios across
  machines is for later, and is designed then.
- On Linux, axx runs desktops of its own, `desktops` at once (1 by default): each a virtual
  display (Xvfb) with its own session bus and accessibility bus, and its own homes for the
  apps. Nothing shows on your screen, and apps run under X11 (GTK, Qt and Chromium choose it
  when there is no Wayland display). With `--watch`, they run on your display, one at a time.
- With `display: wayland`, each scenario's desktop is instead a headless GNOME Shell of its
  own, the desktop most Linux users run, and apps pick Wayland or X11 (on its Xwayland) as they
  do on a GNOME desktop. GNOME Shell's remote desktop takes the pointer's and the keys'
  events; an extension of axx's, run from the session's folder, puts each window at the
  screen's corner and takes screenshots. An app on Wayland knows places in its window only,
  each toolkit from its own corner (GTK 3 its shadow's, GTK 4 its frame's, Qt its content's):
  axx adds the window's place, which GNOME Shell knows.

**The reset.** Before its app starts, a scenario's app is reset. There is no switch that skips
it.

- **Everywhere**: the app's home, a folder of its own in the project's `.axx/desktop`, is
  emptied, and the app starts with it as its home. After the scenario, axx stops the app and
  every process it started. The home is a value of the scenario that other packs' steps can
  use, so the files pack checks what the app saved there (Snap's inbox). That is a core
  addition: packs give a scenario values its steps expand. Java takes its home from the
  account, not `HOME` (proven on JDK 25), so axx sets `-Duser.home` for it through
  `JAVA_TOOL_OPTIONS`, which moved it.
- **macOS**: `HOME` and `CFFIXED_USER_HOME` point at the home. Foundation ignores `HOME` alone:
  on macOS 27 an app's home, Application Support and Documents stayed the person's, and with
  `CFFIXED_USER_HOME` they moved into the scenario's home. Preferences move with neither: they
  are the user's, kept by cfprefsd, and a preference one run wrote was there in the next. So
  the app's preferences (its bundle identifier's domain) are emptied before each scenario, and
  the person's own are kept aside during the run and put back at its end. Window restoration
  is off (`-ApplePersistenceIgnoreState YES` at launch). A sandboxed app keeps its data in its
  container (`~/Library/Containers/<bundle id>`), which is emptied and put back the same way.
  The keychain is not reset: a sign-in an app keeps there outlives the scenario, and the docs
  say so.
- **Windows**: `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, `TEMP` and `TMP` point into the home.
  Apps' data folders follow them: .NET's and the shell's ApplicationData and
  LocalApplicationData resolved into the home. The profile and Documents do not, as Windows
  resolves them for the user. The registration's `registry` keys are emptied before each
  scenario, the person's own kept aside and put back. A packaged app's data belongs to its
  package, beyond any variable, so the package is reset (`Reset-AppxPackage`) before each
  scenario.
- **Linux**: `HOME` and the XDG directories point into the home, and the desktop's session bus
  runs with them, so the app's settings (dconf), keyring and portals start empty too.
- What is kept aside is put back when the run ends, and by the next run if one ended abruptly.

**Switched on by axx, for every app it starts**, never in the app:

- macOS: `AXEnhancedUserInterface` on the app, as VoiceOver sets it (Flutter builds its
  semantics for it).
- Windows: the Java Access Bridge, for Java apps (`JAVA_TOOL_OPTIONS`), read through its DLL.
- Linux: the assistive-technology announcement (`org.a11y.Status`); the accessibility bus's
  address on the root window (Qt 5); java-atk-wrapper, for Java apps (`JAVA_TOOL_OPTIONS`);
  and Chromium's accessibility (Electron, CEF), by its environment if Chromium takes it there,
  or else by `--force-renderer-accessibility` in the registration's `args`.

### Screenshots

`the depot desktop app looks like the "register" screenshot` compares the app's front window
with `screenshots/register.<platform>.png`: `darwin`, `windows` or `linux`, as the web pack
names them, with `@2x` on a display at twice the scale. Each platform draws windows its own
way, so each has its own screenshots, compared on it only, and `platforms` names those a
project keeps. The first time, the step takes the screenshot and fails: look at it, and keep
it. The window is captured by `screencapture -l` on macOS (Screen Recording), by `PrintWindow`
with its full content (WebView2's too) on Windows, and from the X server on Linux (from GNOME
Shell, on Wayland).

### Checks before a run

`axx doctor`, and the first desktop step of a run:

- macOS: Accessibility and Screen Recording (Device Control & Data Access, on macOS 27) for the
  app at the top of the process tree, which the hint names (the terminal, or the IDE).
- Windows: a desktop to run on: not a service, not an SSH session (a scheduled task for the
  signed-in user has one). For Java apps, a JDK's Java Access Bridge (a warning).
- Linux: Xvfb, dbus-daemon and at-spi2-core. For Java apps, java-atk-wrapper (a warning). For
  Wayland desktops, gnome-shell and pipewire (a warning).

### In CI

As Phase 1 found the hosted runners: `macos-latest` and `windows-latest` as they come, and
`ubuntu-24.04` with `xvfb` and `at-spi2-core` installed (axx starts them), or GNOME Shell for
Wayland. Screenshots are pixels, which follow the libraries that draw them: CI's `desktop` job
runs the depot desk's feature (`examples/parcels/depot-desk`) against every Linux build, on X11
and on Wayland, in the desk's own image (`examples/parcels/depot-desk/linux`), built once for
each version of its Dockerfile and kept in the registry, so CI and developers draw the same.
The macOS and Windows builds run on developers' machines: hosted runners change with their
images, which no one can run elsewhere (owner's decision, 2026-10-06).

### Not in this design

- Running scripts in webviews, and an app's internal state (above).
- Wayland desktops other than GNOME's (KDE's, wlroots'); on them, apps are watched under
  Xwayland.
- More than one screen; the menu bar's extras, the Dock, the taskbar and the tray; hotkeys
  the system takes before an app does. Snap's overlay is started by its `--overlay-mode`
  argument, not its hotkey.

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

   *Hosted CI, settled on 2026-10-04* by a probe workflow on the `ci/desktop-probe` branch,
   which reads a copy of Snap's toolbar in a web view of each OS's engine (a WKWebView app in
   Swift, Edge, a WebKitGTK 4.1 window in Python) and presses three of its buttons. Every
   hosted runner runs the drivers as it comes:
   - **macOS** (`macos-latest`): the job is allowed Accessibility and Screen Recording
     already (System Integrity Protection is off; the job runs under the runner's
     hosted-compute-agent, Runner.Listener and Runner.Worker), so nothing is granted.
   - **Windows** (`windows-latest`): the job runs as `runneradmin` in the active console
     session, on a desktop, and UI Automation works from a step. PowerShell splits an
     argument like `-test.v` at its dot, so such arguments are quoted.
   - **Linux** (`ubuntu-24.04`): with `xvfb`, `dbus-x11` and `at-spi2-core` installed, and a
     display, a session bus and the accessibility bus started, AT-SPI reaches WebKitGTK 2.52
     with its sandbox on.
   *Native apps and the frameworks that need care, proven on 2026-10-04* (`native_*_test.go`
   in each driver), each on an app as it is: a depot desk window (a Reference field, a
   Register button, a status line) typed into, its button pressed and its status read, or a
   calculator's 7 + 5 = read as 12.

   | | macOS | Windows | Linux |
   |---|---|---|---|
   | Native toolkits | AppKit (TextEdit: text, a menu command, a system dialog), SwiftUI (Calculator) | Win32 (WinForms), WPF, WinUI (Calculator, packaged) | GTK 3, GTK 4 (GNOME Calculator, libadwaita) |
   | Web views | WKWebView (Tauri) | WebView2 (Tauri) | WebKitGTK (Tauri) |
   | Electron 44 | read through AX, clicked with the pointer | as is | `--force-renderer-accessibility` at launch |
   | Java Swing (JDK 21) | typed with the keyboard | through the Java Access Bridge, switched on for the launch (`JAVA_TOOL_OPTIONS`) | java-atk-wrapper, on the boot class path (`JAVA_TOOL_OPTIONS`) |
   | Flutter 3.47 | semantics on with `AXEnhancedUserInterface`; typed with the keyboard | as is; typed with the keyboard | its Focus action, typed with the keyboard |
   | Qt 5.15, Qt 6 | as is (PyQt: Qt 5.15.14, 6.11) | as is (PyQt: Qt 5.15.2, 6.11) | Qt 6 as is (6.4); Qt 5 (5.15.8: PyQt; FeatherPad, a C++ app, read) with the bus address on the X root window |

   What it teaches the design:
   - **Real input first.** A person clicks and types; apps may ignore the accessibility
     equivalents, or take them and do nothing. Chromium on macOS ignores `AXPress`; Java on
     macOS ignores a set `AXValue`; Flutter on macOS reports a set `AXValue` back while its
     app never gets it; Flutter on Linux takes `SetTextContents`, changes nothing and stops
     answering. So clicks and typing are pointer and keyboard events (CGEvent, SendInput,
     XTest) on the frontmost app, and accessibility actions are the fallback, for an element
     out of reach of the pointer.
   - **axx switches on what each framework needs, at launch or from outside**, never in the
     app: the assistive-technology announcement on Linux (`org.a11y.Status`), Chromium's
     `--force-renderer-accessibility` on Linux, Java's bridge through `JAVA_TOOL_OPTIONS`,
     `AXEnhancedUserInterface` on a macOS app (Flutter), and the accessibility bus's address on
     the X root window (`AT_SPI_BUS`) on Linux.
   - **Qt 5 on Debian and Ubuntu** (Debian 12 and 13, Ubuntu 24.04) carries a distribution
     patch (`a11y_root.diff`) that registers the app with the accessibility bus on its first
     event loop, before Qt has the bus's address from D-Bus; the registration fails and Qt
     never tries again, so the app is on no bus. Qt reads the address from the root window's
     `AT_SPI_BUS` property first when it is there, and at-spi-bus-launcher puts it there, but
     a bare Xvfb resets, and loses it, when its last client leaves. So axx puts the address on
     the root window itself and keeps its connection to the display open while apps run; a
     desktop session keeps the launcher's.
   - **Launching**: macOS kills a system app started from its executable, so a `.app` is
     opened through LaunchServices; a packaged Windows app's window belongs to the frame
     host, so windows are matched by the app, not only the process that started it.
   - **Stopping**: an app's helper processes outlive it (Electron's renderers wrote to the
     scenario's home after the app was stopped), so axx stops its whole process tree.
   - **Isolation gaps**: sandboxed macOS apps and packaged Windows apps keep their data in
     a container of their own, which a scenario's home does not reach (Calculator showed the
     last calculation). The pack's reset empties those containers too.
   - **Names**: Flutter on macOS gives a text field no name at all (Windows and Linux give its
     label), so a step needs another way to it (its order, or an identifier); WebKitGTK and
     Flutter on Linux need numeric roles and actions read one by one, and Flutter on Linux
     reports no positions.
2. **Design**: the pack's shape, the registration, the step text (approved before building),
   the isolation and the lease. This ADR is then accepted or amended.
   *Designed, and accepted with its step text, on 2026-10-04*: The packs, above. Facts proven
   for it: on macOS 27, Foundation ignores `HOME` and follows `CFFIXED_USER_HOME`, while
   preferences follow neither; on Windows 11, apps' data folders follow `APPDATA` and
   `LOCALAPPDATA`, while the profile, Documents and the registry do not; Java ignores `HOME`
   and follows `-Duser.home`.
3. **Build** the desktop packs: `desktop-core` and the three drivers behind one interface,
   locating, waiting, actions, input, screenshots, the reset, the desktop lease, Linux's
   desktops, and doctor's checks. Per ADR 0012: `app-core` first, with the mobile packs moved
   onto it; desktop's packs under it; `services` for `axx.yaml`'s `apps`; and file contexts in
   the files pack.
   Every step is proven on each OS, and every part of the reset: preferences,
   sandbox containers and window restoration on macOS, registry keys and packaged apps on
   Windows, the session bus's settings and keyring on Linux, and whether Chromium on Linux
   takes its accessibility from the environment.
   *Built on 2026-10-05*: `app-core` (the mobile packs on it), `desktop-core`, `desktop-macos`,
   `desktop-windows` and `desktop-linux`. One journey of the depot desk, every step through the
   packs' steps with a restart and a new scenario's reset, a file of the app's read through its
   file context and its window captured (`packs/desktop/internal/desktest`), passes on 6 builds
   on macOS (all but Qt 5 and Qt 6, below), all 9 on Windows and all 8 on Linux; two Linux
   desktops run two scenarios at once, each app's home at one path in both. What the packs
   learned beyond the walks:
   - **What a toolkit does not say fails its check, saying so.** Flutter says every control of
     its own is enabled on macOS and Windows (its menus on macOS are AppKit's, which say), so
     there a control's enabled state counts as not reported; on Linux, a Flutter control is
     enabled when it offers a click. Java on Linux does not report what is typed into a field:
     a fill is not read back, and the field's value check fails, saying why.
   - **Only controls that take input are waited for to be enabled**: Java reports its table's
     rows disabled, and a click selects them.
   - **One element is counted once**: WPF and Java give an open menu's item under the menu and
     again in its popup.
   - **A menu is what opens one**: a menu bar's item, whatever its role (a menu in GTK 3 and
     Java, a menu item in GTK 4 and Qt, a button where Chromium draws Electron's menu bar on
     Windows and Linux), or an item that opens a menu (WPF's menu bar is a menu). On Linux, a
     menu that does not open takes another click: a menu bar just used (Swing's, after a choice
     made through the bridge) takes the first for closing.
   - **Chromium on Linux builds its tree from the environment** (`ACCESSIBILITY_ENABLED`) but
     says nothing in it shows: an Electron app's registration has
     `--force-renderer-accessibility` in its `args`.
   - **Linux**: each scenario has its own session bus and accessibility bus, started with its
     app's home, and a runtime folder (`XDG_RUNTIME_DIR`) outside the desktop's view, where the
     buses' sockets are. With more than one desktop, the session runs in a user and mount
     namespace whose first process is axx itself, run again: it mounts the desktop's own
     folder as the project's `.axx/desktop/linux`. Docker's default seccomp profile forbids
     these namespaces; `axx doctor` says so.
   - **Windows**: UI Automation and the Java Access Bridge are called on one thread, which
     handles the bridge's messages between calls.
   - **Screenshots leave out a focused field's text cursor**, as the web pack's do: it blinks,
     and a screenshot kept with it on did not match the app with it off for 20 seconds
     (SwiftUI, one run in three). Each driver finds it where its tree says: AX's bounds of the
     insertion point, AT-SPI's character extents at the caret (in the innermost element that
     has the focus: a web view has it, and so does the field in its page), UI Automation's
     selection, the Java Access Bridge's caret. AppKit's empty fields put the line above them,
     so a line outside its field is the field's height. GTK 4.14 gives no character extents,
     Flutter on Windows no text pattern, Swing on macOS no bounds for an empty field's
     insertion point: their cursors stay in screenshots.
   - **A shortcut's modifiers are keys a hand holds down** (macOS): flags on the key's own
     events left Control held for every click after Control+Shift+S, and macOS's region
     capture, dragged then, copied to the clipboard instead of saving.

   Not yet: Qt keeps its settings on macOS in a preferences domain of its own
   (`com.parcels-example.Depot desk`), which no registration names, so Qt's second scenario
   starts with the first's service level; sandboxed apps' containers on macOS and packaged apps
   on Windows (the reset of each, and starting a packaged app); dconf and the keyring, which
   the depot desk does not use.
4. **The baseline, modelled and documented**: the depot desk in `examples/parcels`, built with
   every toolkit Phase 1 proved, on each OS where it runs: AppKit, SwiftUI, Tauri, Electron,
   Qt 5, Qt 6, Swing and Flutter on macOS; WinForms, WPF, WinUI, Tauri, Electron, Qt 5, Qt 6,
   Swing and Flutter on Windows; GTK 3, GTK 4, Tauri, Electron, Qt 5, Qt 6, Swing and Flutter
   on Linux. Every build is the same desk: a depot clerk registers the parcels arriving at the
   depot, sees the day's arrivals, has the courier sign the handover, and closes the day from
   a menu. Each keeps the arrivals in its data folder and remembers the last service level as
   its toolkit keeps settings, so each also proves the reset. One feature, with a scenario for
   every step, runs against every build, and CI runs it on each OS: the baseline that keeps
   every toolkit working. Then a guide, the generated reference and the skills.
   *Under way on 2026-10-04*: the desk is built with every toolkit (`examples/parcels/depot-desk`),
   and a walk for each OS's driver works it as a clerk does, a restart and a reset included,
   ahead of the packs' steps. *On 2026-10-05* every build passes: 8 on macOS, 9 on Windows
   (Windows 11 at 175%) and 8 on Linux. The desk's Arrivals tab is one column taller than its
   640 by 580 point window: it scrolls as a page, and Expected today starts below the window's
   edge, so a row of it is reached by scrolling the page, then the table. On the laptop at
   175% the window does not fit above the taskbar (569 points of height), and is fitted. What
   the toolkits taught the drivers, beyond Phase 1:
   - **Roles differ, the same way each time**: a list on macOS is a table (AppKit, SwiftUI);
     Qt's and Swing's links are texts; GTK 4.14 reports radio buttons and switches as check
     boxes; Flutter's texts, tabs and rows are panels, with their place ("Tab 1 of 2") on a
     second line; Java's list items and cells on Linux are their renderers' labels. The
     packs map roles per OS, and a few controls are found by name whatever their role.
   - **Places**: GTK 4.14 knows places in its window only; Flutter on Linux has none, and
     hangs when asked (it is told apart by its embedder in its process); Java's menus give
     places in their popup, its frame none. With no place, a control takes its own action.
   - **What is shown and what is there**: Java and Chromium keep hidden or scrolled-away
     controls in the tree; Qt 5 on macOS and GTK 3 have only what shows; GTK 4 passes a row's
     element to the rows scrolling into its place. A control is found by its name after any
     scroll, and is in view when a hit test at its middle reaches it (from the page, in
     Chromium; by its viewport, where Java's hit test stops short).
   - **Scrolling, area by area**: an area's middle may be covered by a list, which takes the
     wheel's turns, so the wheel turns where no list is. Toolkits report what an area clips
     differently: Flutter squashes what is scrolled away to a line a point high on the area's
     edge (macOS); GTK 3 gives what it has not drawn no place, so an area is looked for down
     to the end, then up; WebKitGTK moves its scroll panes' places with what they scroll, so
     in a web page the page's own hit test decides. GTK 4's hit test finds what an area clips
     away, and a notebook's hidden page, which it says shows: there, the areas' places alone
     decide. WPF holds a tab's content in its tab item, the size of its header, so only areas
     clip; Qt says its tables are 0% scrolled and are not asked to scroll, so a scroll is seen
     by what shows. A field's value is read wherever the field is: scrolled away, GTK 3 and
     UI Automation say it does not show.
   - **Keys**: Qt on macOS ignores key events that carry only a character, so typing sends
     each character's key; Java on Linux loses keys when another window takes the keyboard,
     so axx gives a window the keyboard only when no app took it; Java's bridge on Linux
     does not report what is typed into a field.
   - **Windows**: WinUI 3 takes no wheel turns that `SendInput` makes, and places a new
     window anywhere, part of it under the taskbar (a control is in view only in the screen's
     work area); its lists take UI Automation's Scroll pattern, a page at a time. Flutter's
     engine for Windows serves only MSAA (its UI Automation is off by default), and its tree
     never follows a scroll ([flutter/flutter#189124](https://github.com/flutter/flutter/issues/189124),
     open): the wheel scrolls its list, but every control keeps the place it had before. So a
     control out of view takes its default action, as MSAA gives it (Flutter's tap), and is
     never scrolled to; once a Flutter view has scrolled, no place in it is true. Through the
     Java Access Bridge, a JTable's
     children and its table API's cells are all its one cell renderer (the last cell drawn,
     with no place); its visible children are the cells themselves.
   - **Reset**: Java's preferences on macOS are one domain for every Java app (an IDE's
     among them), so no reset can empty one app's: the Swing desk keeps settings in a file,
     and the docs name the limit.

   *On 2026-10-06* the feature runs with the axx CLI (`examples/parcels/depot-desk/acceptance`,
   a profile for each build), and on Wayland every Linux build runs in a GNOME Shell of its
   own (`--profile linux-<build>,wayland`): GTK 3, GTK 4, Qt 6, Electron, Tauri and Flutter on
   Wayland, Qt 5 and Swing on its Xwayland, as a GNOME desktop starts them. CI's `desktop`
   job runs the feature against every build. What Wayland and the runs taught:
   - **Places on Wayland**: an app knows places in its window only, each toolkit counting
     from its own corner, and GNOME Shell knows where its windows are. Only GTK 4 counts in
     its window's coordinates when asked for the screen's; Qt 5 under a window manager gives
     wrong places in its window's, so every other app is asked for the screen's. GTK 3's
     least integer for what it has not drawn stays as it is.
   - **Toolkits pick their display as on GNOME**: Electron 44 takes Wayland from the session
     (`XDG_SESSION_TYPE`), not from its old hint; Debian's and Ubuntu's Qt 5 is read on X11
     only (Qt 5 on Wayland never registers), so it runs on Xwayland, as it does there without
     qtwayland5. X11 apps read the accessibility bus's address on Xwayland's screen, which
     takes GNOME Shell's cookie.
   - **Keys**: GNOME Shell's remote desktop drops the modifiers of the first key it takes
     (Control+A typed an "a"): a Shift, pressed and let go as the session starts, is that key.
   - **Scrolling**: GTK 3 gives a tab only its header's place, and the scroll bars of the
     scroll pane it holds made it an area; an area's bar is its own. A web control's middle
     must be in the page's view: Chromium's hit test above its page, under the window's menu
     bar, finds what is scrolled there.
   - **Screenshots**: a text cursor an app does not place (Flutter's on Linux) is seen
     blinking: all that changes between two looks is a column a line high, and it is left
     out from then on; at a display's scale a cursor's place is rounded a pixel either way,
     so it is hidden with the color around it, never a letter's edge. Windows shows a new
     window's keyboard cues (Windows Forms' focus rectangle) when an earlier scenario's last
     input was a key: an app's window hides them once it is in front (`WM_CHANGEUISTATE`;
     sent sooner, Windows' own setting as it activates the window may come after), as one a
     person opens with the mouse does. A capture waits for the window, as a click does: UI
     Automation lists a window a moment late at times. WinUI says its window is at 96 dots
     an inch and, captured, draws itself at that size at the top left of its pixels: the
     capture is cropped to what it drew (a screenshot at scale 1), and a click at a place
     takes the display's scale, the monitor's.
   - **Settings outside the app's home**: on macOS, Qt's `QSettings` keeps them in a domain
     named after the app's organization, not its bundle: the registration's `preferences`
     names the domains to empty, as Windows' `registry` names keys. Linux needs neither: an
     app's settings are in its home.
   - **Qt on macOS leaves its scroll areas out of the tree**, their content in the group that
     holds them: a group an element lies outside of is an area that scrolled it away.
   - **Traces are asked for**: a capture after every step slowed a run by a fifth on macOS,
     where `screencapture` is a process of its own. Kept as it writes it, and taken while the
     next step finds what it acts on (which acts once it is done), it slows one by a tenth.
5. **Snap moves to it**: its features use the desktop steps, and the `acceptance` feature and
   its WebDriver plugin go. Snap names its saves by the time and checks their pixels, which
   the files pack cannot check yet (a folder's files counted or matched by a pattern, and a
   saved image compared with a screenshot): suggested to the owner separately.

## Consequences

- axx tests desktop apps on all three operating systems the way it tests mobile apps, with no
  build wired for testing and no code for any framework.
- axx owns three accessibility drivers. Their surface is small (read the tree, act, type,
  capture), but each OS has its own edge cases.
- Hosted CI runs the drivers on all three OSes as it comes (Phase 1).
- On macOS and Windows, desktop scenarios take the machine's screen, pointer and keyboard while
  they run, one at a time, until they can run across machines. On Linux they run in desktops
  of axx's own, several at once.
- The depot desk is built with every toolkit, on every OS, in CI: a baseline that costs build
  time, and keeps each toolkit's support proven.
- The reset reaches outside the scenario's home: the app's preferences on macOS, its registry
  keys on Windows. The person's own are kept aside and put back, but a developer who also uses
  the app under test sees it start clean during a run.
- Apps run under X11 on Linux, so Wayland-only apps are out of reach.
