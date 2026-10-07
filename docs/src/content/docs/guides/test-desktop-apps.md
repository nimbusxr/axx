---
title: Test desktop apps
description: "Use your macOS, Windows and Linux apps the way people do, through the accessibility tree screen readers read: click and fill their controls by the names people see, press keys, draw, check what they show; each scenario with a clean app, and one feature for every OS."
---

Many acceptance criteria describe what a person does in a desktop app: a depot clerk registers the parcels that arrive, has the courier sign for them, and closes the day. The `desktop-macos`, `desktop-windows` and `desktop-linux` packs do exactly that, with the app as it ships: they read it through the operating system's accessibility tree, what screen readers read, and click and type with the pointer and the keyboard, as a person does. Nothing is built into the app for testing.

```gherkin
Scenario: A clerk registers a fragile express parcel with a label
  Given the depot app is launched
  When the "Reference" field in the depot app is filled with "PX-DSK-4201"
  And the "Fragile" checkbox is clicked in the depot app
  And the "Express" element is clicked in the depot app
  And the "Print label" element is clicked in the depot app
  And the "Register" button is clicked in the depot app
  Then the depot app shows "Registered PX-DSK-4201: Express, fragile, label printed"
  And within 5s the "arrivals.json" file in the desk folder contains "PX-DSK-4201"
```

The steps are the same on every OS, and the same as a phone app's: only the app's registration says which OS it is for. Add the pack for each OS you test with `axx pack add desktop-macos`, `axx pack add desktop-windows` or `axx pack add desktop-linux` (each builds on `desktop-core`, the steps only desktops have, and on `app-core`, the steps every app has; [Choose packs](/guides/use-packs/)).

The parcels example's depot desk (`examples/parcels/depot-desk`) is one desk built with every toolkit the packs cover, and one feature runs against every build: AppKit, SwiftUI, Qt, Swing, Electron, Tauri and Flutter on macOS; Windows Forms, WPF, WinUI, Qt, Swing, Electron, Tauri and Flutter on Windows; GTK, Qt, Swing, Electron, Tauri and Flutter on Linux.

## What it needs

- **macOS:** Accessibility and Screen Recording for the app that runs axx: your terminal, or your editor. axx reads other apps and takes their screenshots only with them (System Settings, Privacy & Security).
- **Windows:** a desktop to run on: a signed-in user's session, not a service or an SSH session (a scheduled task for the signed-in user has one). axx brings an app to the front as the terminal it runs in lets it: run it from a terminal in front, or have the scheduled task open a console with `conhost.exe` (a task's Windows Terminal cannot come to the front itself, and flashes instead: a taskbar that hides itself then shows in every screenshot). Java apps need a JDK's Java Access Bridge, which comes with it.
- **Linux:** Xvfb, dbus-daemon and at-spi2-core (Debian and Ubuntu: `apt install xvfb dbus at-spi2-core`), and for Java apps java-atk-wrapper (`apt install libatk-wrapper-java-jni`). axx runs desktops of its own, so nothing shows on your screen. For Wayland, GNOME Shell (below).

`axx doctor` checks them, and names what is missing.

## Register the app

A feature registers the app once for each OS it runs on, and runs unchanged on each: a run uses the registration for the OS it runs on.

```gherkin
Background:
  Given the depot macos app with the following properties:
    | app | ../appkit/build/Depot desk.app |
  And the depot windows app with the following properties:
    | app      | ../winforms/bin/Release/net8.0-windows/DepotDesk.exe |
    | registry | Software\Parcels\Depot desk                          |
  And the depot linux app with the following properties:
    | app  | python3          |
    | args | ../gtk4/desk.py  |
```

| Property | What it is |
| --- | --- |
| `app` | The app. macOS: a `.app` of the project, an installed app's bundle identifier (`com.apple.TextEdit`), or an executable. Windows: an `.exe` of the project, or a command. Linux: an executable of the project (an AppImage too), or a command. |
| `args` | The arguments it starts with; a quoted one with spaces. A file of the project is relative to `axx.yaml`, as an option's value too (`--config=depot.yaml`). |
| `env.<name>` | An environment variable it starts with, like `env.PARCELS_API`. |
| `registry` (Windows) | The keys under `HKEY_CURRENT_USER` the app keeps its settings in, emptied before each scenario. |
| `preferences` (macOS) | The preferences domains the app keeps its settings in beyond its bundle identifier's, emptied before each scenario: Qt's `QSettings` names its own, after the app's organization. |
| `locale`, `timezone` | The language and region (`de-DE`) and the time zone (`Europe/Berlin`) it starts in. Windows has neither of an app's own: there the machine's apply, and the run says so. |

The app starts when a step launches it. Every step names the app, so a scenario can drive two, and a web app or a phone app too. Where the app a step is for differs between platforms, a property can name it: `in the ${sys:screen.app} app`.

## Each scenario has a clean app

Before the app starts in a scenario, what it keeps is emptied: its home, a folder of the project's `.axx/desktop`, and what its OS keeps of it elsewhere (on macOS its bundle identifier's preferences and the `preferences` it names, on Windows its `registry` keys; on Linux its settings are in its home). Your own settings in those places are kept aside during the run and put back when it ends. After the scenario, the app and every process it started are stopped. There is no switch that skips it: a scenario is one journey, and that journey is its own.

A desktop has one screen, one pointer and one keyboard focus, so on macOS and Windows desktop scenarios take the machine's desktop in turns, while the run's other scenarios go on alongside them. On Linux, axx runs desktops of its own, `desktops` at once (1 by default), each with its own buses and its own homes for the apps.

```yaml title="axx.yaml"
packs:
  desktop-linux:
    desktops: 2   # two desktop scenarios at once
```

## Its files

An app's files are a folder of the files pack whose `owner` is the app: `./` is where its OS keeps an app's data, `~/` is its home ([Check files](/guides/check-files/)).

```gherkin
Given the desk folder with the following properties:
  | owner | app:depot     |
  | path  | ./Depot desk  |
Then within 5s the "arrivals.json" file in the desk folder contains "PX-DSK-4205"
```

## Launch it

```gherkin
When the depot app is launched
When the depot app is restarted
```

A restart keeps what the app stored, as quitting it and opening it again does; the next scenario's reset does not.

An app that waits in the tray, with no window until it is used, is launched once its icon is there: in the menu bar's status area on macOS, in the tray of axx's desktops on Linux (a StatusNotifierWatcher, as KDE and GNOME with AppIndicator support keep one). The icon is a `menu`, named by its tooltip, and the entries of the menu it opens are `menu item`s:

```gherkin
When the "Courier tracker" menu is clicked in the tracker app
And the "Pause tracking" menu item is clicked in the tracker app
```

### Apps the system runs

What an app asks of the system shows in an app the system runs: the file manager it opens a folder in. Register that app with `owner: system`, once for each OS, and the scenario reads it as it runs. It is never reset, launched or stopped: the windows it shows during the scenario are closed as the scenario ends, as a person closes them, and the app runs on. On Linux, the desktop's own starts on axx's desktop when an app asks for it.

```gherkin
Given the files macos app with the following properties:
  | app   | com.apple.finder |
  | owner | system           |
And the files windows app with the following properties:
  | app   | explorer.exe |
  | owner | system       |
And the files linux app with the following properties:
  | app   | nautilus |
  | owner | system   |
When the "Courier tracker" menu is clicked in the tracker app
And the "Show the day's log" menu item is clicked in the tracker app
Then within 10s the files app shows "Courier logs"
```

## Click, fill and press keys

```gherkin
When the "Register" button is clicked in the depot app
When the "Reference" field in the depot app is filled with "PX-DSK-4202"
When the Enter key is pressed in the depot app
When the ControlOrMeta+A key is pressed in the depot app
When the "PX-DSK-4138" row is scrolled into view in the depot app
When the "Depot" menu is clicked in the depot app
And the "Close day" menu item is clicked in the depot app
```

Controls are found by the names people see and screen readers read: a button by its text, a field by its label, a list item or a row by one of its texts, an image by its description. The kinds are `button`, `field`, `checkbox`, `radio button`, `switch`, `tab`, `menu`, `menu item`, `list item`, `row`, `link`, `image`, `text` and `element` (anything, by its text or its accessibility label). Toolkits draw some controls differently (a switch in one is a checkbox in another), so a feature that runs against several names those an `element`. When no name tells a control apart, `id=` (its accessibility identifier on macOS, its AutomationId on Windows, its `id` attribute on Linux) or `xpath=` finds it.

A control is clicked where it shows, after scrolling it into view as a person would. Keys go to the app's focused window, named as the web pack names them: `ControlOrMeta` is Command on macOS and Control elsewhere, so a shortcut runs unchanged on each OS.

What an app draws rather than names, like a signature pad or a map, is clicked or dragged at a place on it, in points from its top left:

```gherkin
When the "Courier signature" element in the depot app is clicked at 40, 40
When the pointer is dragged from 40, 30 to 140, 30 on the "Courier signature" element in the depot app
```

What keeps its place in the middle of a control, or by one of its corners, as the control grows with the window or the screen (a picture shown in the middle, a button in a corner) is placed from there instead: from its `middle`, `top left`, `top right`, `bottom left` or `bottom right`. Places run right and down, so one left of or above that point is negative:

```gherkin
When the pointer is dragged from -60, 10 to 60, -10 from the middle of the "Courier signature" element in the depot app
When the "Courier signature" element in the depot app is clicked at -40, -30 from its bottom right
```

A place is on the control, or the step fails: a click off it would reach something else (a drag may end off it). Points are the screen's, so an app that lays itself out in pixels and does not grow with Windows' display scale (Windows Forms and Qt 5 apps that do not ask to) is smaller in points on a screen at 250%: the depot desk's signature pad, 400 by 160 pixels, is 160 by 64 points there, and its scenarios sign inside that.

## Check what it shows

```gherkin
Then the depot app shows "No parcels registered yet"
Then the depot app does not show "PX-DSK-4211"
Then the "Register" button is disabled in the depot app
Then the "Reference" field in the depot app has the value ""
Then the "Leipzig depot" image is shown in the depot app
Then within 20s the depot app looks like the "opened" screenshot
```

Checks wait for the app, 10 seconds or `within {duration}`. A failed step attaches the app's window and its controls, as accessibility has them.

**Screenshots** are of the app's front window, as it draws itself, in front and with the pointer away from it: `<name>.<platform>.png` in the project's `screenshots` folder, with the display's scale when it is not 1 (`opened.darwin@2x.png`). Each platform draws windows its own way, so each has its own, compared on it only. A focused field's text cursor is left out: it blinks. The first time, the step takes the screenshot and fails: look at it, keep it, and run again. `packs.desktop-core.screenshots` in `axx.yaml` sets the `folder`, the `tolerance`, the `platforms` a project keeps, and `update: true`, which takes them again.

## Traces and videos

Ask for a **trace** of each app, and a scenario keeps one in `.axx/desktop/traces`: a page with its window after each step, and its controls where it failed. **Videos** of the window, as animated PNGs, go in `.axx/desktop/videos`. A trace captures the window after every step, while the next step finds what it acts on: kept for failed scenarios only, it still slows every step a little (about a tenth, on macOS), so it is off until you ask.

```yaml title="axx.yaml"
packs:
  desktop-core:
    traces: failed   # always, failed or never (the default)
    videos: failed   # always, failed or never (the default)
```

## Toolkits report differently

Every toolkit reports to accessibility in its own way, and some leave things out: Flutter says every control is enabled on macOS and Windows, Java on Linux does not report what is typed into a field, Flutter on Linux gives no places on the screen. A scenario that checks what a toolkit does not report is tagged, and the profile for that build leaves it out, as the depot desk's `axx.yaml` does:

```yaml title="axx.yaml"
profiles:
  macos-flutter:
    run: {tags: not @enabled-state}
    properties: {desk.app: ../flutter/build/macos/Build/Products/Release/Depot desk.app}
```

Each build's README in `examples/parcels/depot-desk` says what its toolkit gives accessibility.

## Linux: X11 or Wayland

axx's Linux desktops are X11 (Xvfb) by default. With `display: wayland`, each scenario's desktop is a headless GNOME Shell of its own instead, the desktop most Linux users run, and apps pick Wayland or X11 as they do on a GNOME desktop: GTK, Qt 6 and Electron run on Wayland, Java and Qt 5 on its Xwayland. It needs gnome-shell and pipewire, and xwayland for X11 apps (`apt install gnome-shell pipewire xwayland`), and a system bus.

```yaml title="axx.yaml"
profiles:
  wayland:
    packs: {desktop-linux: {display: wayland}}
```

`axx run --profile wayland` runs the Linux desktop scenarios on Wayland. Screenshots on Wayland show GNOME's window frames, so keep them in a folder of their own (`packs.desktop-core.screenshots.folder`).

## Watch it

`axx run --watch` runs the desktop scenarios where you can see them, one at a time, slowed down with `--slowdown 500ms` ([Watch a run](/guides/watch-runs/)). On Linux they then run on your own display.

## In CI

On a Linux runner, axx needs only its packages (`xvfb`, `dbus` and `at-spi2-core`; GNOME Shell and PipeWire for Wayland), and starts the desktops itself:

```yaml
runs-on: ubuntu-24.04
steps:
  - uses: actions/checkout@v7
  - run: sudo apt-get update && sudo apt-get install -y --no-install-recommends xvfb dbus at-spi2-core
  - run: axx run --profile linux-gtk4
```

Screenshots are pixels, and pixels follow the libraries and fonts that draw them: a runner draws as its image does this week. Run the scenarios in an image of your own, and every run and every developer draws the same: axx's CI runs the depot desk's Linux builds that way, in the image of `examples/parcels/depot-desk/linux`, built once for each version of its Dockerfile and kept in the registry (the `desktop` job in `.github/workflows/ci.yml`). Hosted macOS and Windows runners change with their images, and draw at a scale of 1: their screenshots are not the ones a developer's machine keeps.
