# Depot desk

The desk at the parcels service's Leipzig depot. A clerk registers the parcels arriving at the
depot, finds them on the day's list of expected parcels, has the courier sign for the
handover, and closes the day.

The desk is built with every desktop toolkit the desktop packs cover, each build the same desk
with the same names (ADR 0011). One feature runs against every build on its OS: the baseline
that keeps each toolkit working.

| Build | Toolkit | Runs on | Written in |
| --- | --- | --- | --- |
| `appkit` | AppKit | macOS | Swift |
| `swiftui` | SwiftUI | macOS | Swift |
| `winforms` | Windows Forms | Windows | C# |
| `wpf` | WPF | Windows | C# |
| `winui` | WinUI 3 | Windows | C# |
| `gtk3` | GTK 3 | Linux | Python |
| `gtk4` | GTK 4 | Linux | Python |
| `qt` | Qt 5 and Qt 6 | macOS, Windows, Linux | Python (PyQt5, PyQt6) |
| `swing` | Swing | macOS, Windows, Linux | Java |
| `electron` | Electron | macOS, Windows, Linux | JavaScript |
| `tauri` | Tauri 2 | macOS, Windows, Linux | Rust, HTML |
| `flutter` | Flutter | macOS, Windows, Linux | Dart |

Each build's folder has its sources and its `README.md`, which says how to build and start it.

## Its acceptance tests

`acceptance/features/depot-desk/depot-desk.feature` is the clerk's day with the desk, a scenario for
each thing the clerk does, run with axx against every build on the OS it is built for
(`acceptance/features/desk-models` holds the macOS builds' models: [The Mac's own
models](#the-macs-own-models)). Build
the desk first (its folder's `README.md`), then pick the build with its profile:

```sh
cd acceptance
axx run --profile macos-appkit        # macos-swiftui, macos-qt6, macos-electron, ...
axx run --profile windows-winui       # windows-winforms, windows-wpf, ...
axx run --profile linux-gtk4          # linux-gtk3, linux-qt5, linux-flutter, ...
axx run --profile linux-gtk4,wayland  # a Linux build on Wayland, in a GNOME session of its own
```

The Qt builds run with `python3` and the Swing build with `java`: give axx others with
`-D python=...` and `-D java=...`. Each build is drawn with its toolkit's own widgets, so where
toolkits draw a control differently (Print label is a switch or a checkbox, Handover a tab or a
text), the steps name it an element. What a toolkit does not report is tagged, and its
profile leaves those scenarios out: `@enabled-state` (Flutter on macOS and Windows),
`@menu-state` (Flutter on Windows), `@menu-items` (GTK 4.14), `@field-values` (Swing on
Linux) and `@pointer-places` (Flutter on Linux). Each build keeps its screenshots in
`acceptance/screenshots/<build>` (on Wayland, `acceptance/screenshots/wayland/<build>`), one for
each platform and display scale.

### On Linux, in Docker, as CI runs them

`linux/` is an image with every Linux build's tools and both desktops, X11 and GNOME Shell on
Wayland. Its `depot-desk` command builds one build from the sources mounted at `/src` and runs
the feature against it with the `axx` mounted on its `PATH`; `/out`, when mounted, gets the
screenshots and what the run leaves. CI runs it from the registry, the image built once for
each version of the Dockerfile, so its screenshots are the ones kept here:

```sh
docker run --rm --shm-size=1g --tmpfs /run/systemd \
  -v "$PWD:/src:ro" -v "$(command -v axx):/usr/local/bin/axx:ro" -v "$PWD/out:/out" \
  ghcr.io/nimbusxr/axx-depot-desk:<tag> depot-desk gtk4 wayland   # or x11
```

The tag is the first 16 characters of the SHA-256 of `linux/Dockerfile` and `linux/run.sh`
together (`cat linux/Dockerfile linux/run.sh | sha256sum`). The `axx` it mounts is a Linux
build: `GOOS=linux CGO_ENABLED=0 go build ./internal/tools/axxall`.

## The window

The window's title is `Depot desk`, and it is 640 by 580 points. A screen can have less room
than that: at 175%, a laptop's 1920 by 1080 pixels show 1097 by 617 points, 569 of them above
the taskbar. axx then fits the window to the screen, as a person would, and the tab's content
scrolls. Every build gives its controls exactly these names, the ones people see and screen
readers read.

- The image **Leipzig depot**: the depot's logo (`depot.png`), named by its description.
- The menu **Depot**, with the menu item **Close day**, which is disabled while no parcel is
  registered. On macOS it is in the menu bar.
- The tabs **Arrivals**, selected at the start, and **Handover**.

### Arrivals

One column, taller than the window: it scrolls, as a page does. The parcel's form comes
first (the field, the options, Register and the status), then the day's lists: Arrivals, and
under it Expected today, which starts below the window's edge. A clerk scrolls down to it, and
then scrolls its rows.

- The field **Reference**, empty at the start. `Enter` in it registers the parcel; `Escape`
  empties it.
- The checkbox **Fragile**, not ticked at the start.
- The radio buttons **Standard** and **Express**: the parcel's service level. At the start, the
  one registered with last, or Standard.
- The switch **Print label**, off at the start; a checkbox of that name where the toolkit has
  no switch (Qt, Swing, Windows Forms, WPF).
- The button **Register**, disabled while the Reference field is empty. It registers the
  parcel: the status says so, the field empties, Fragile is unticked, and the parcel joins the
  arrivals. A reference already registered today is refused.
- The status, a text:
  - at the start, `No parcels registered yet`, or `3 parcels registered today` (`1 parcel`);
  - after registering, `Registered PX-DSK-4102: Express`, followed by `, fragile` and
    `, label printed` when they apply;
  - for a reference already registered, `PX-DSK-4102 is already registered`;
  - for an arrival clicked in the list, `PX-DSK-4102: Express, fragile` (its level, and
    `, fragile` when it is);
  - after closing the day, `Day closed: 2 parcels handed over` (`1 parcel`).
- The list **Arrivals**: a list item for each parcel registered today, named by its reference,
  in the order they were registered.
- The table **Expected today**: 40 rows, one for each parcel expected, with its reference
  (`PX-DSK-4101` to `PX-DSK-4140`) and its recipient's town. About eight show at once.
  Clicking a row puts its reference in the Reference field.

The towns repeat in this order: Leipzig, Halle, Markkleeberg, Taucha, Schkeuditz, Delitzsch,
Borna, Grimma, Wurzen, Eilenburg.

### Handover

- The signature pad **Courier signature**, 400 by 160 points: a click puts a dot where it is,
  a drag draws a stroke.
- A text, `Not signed`, and `Signed` once the pad has a mark.
- The button **Clear signature**: the pad empties, and the text is `Not signed` again.
- The link **Handover rules**: the text `Parcels are handed over to the courier at 18:00.`
  appears under it.

### Close day

**Depot** > **Close day** hands the day's parcels over: the status says how many, the arrivals
empty, and Close day is disabled again.

## The Mac's own models

The macOS builds (`appkit`, `swiftui`) also use the models the Mac itself has, in
`apple/models.swift`, which both compile:

- **Depot** > **Read label** reads the parcel's reference off the newest scan the depot's label
  camera saved in `Scans`, in the desk's data folder, with Apple's Vision
  (`VNRecognizeTextRequest`), into the Reference field. The status says
  `Read PX-DSK-4301 from the label`, `No reference on the label: type it in` or
  `No scan to read`.
- Registering a parcel with a scan in `Scans` files the scan with the arrival. Vision checks it
  shows a parcel (`VNClassifyImageRequest`: `cardboard_box`, `carton`, `crate` or `envelope`) and
  finds the people in it (`VNDetectHumanRectanglesRequest`), whom it blurs out
  (`VNGeneratePersonSegmentationRequest`) before the desk keeps it as `Photos/<reference>.jpg`.
  The status adds `, photo kept`, `, photo kept, people blurred out` or
  `, photo not kept: it shows no parcel`. The scan leaves `Scans` either way.
- The button **Dictate note**, on the Handover tab, takes the clerk's note through the
  microphone and Apple's speech recognizer (`SFSpeechRecognizer`), and shows it under the
  button: `Listening…`, then `Note: Hand the fragile parcels to the night porter.`
- **Depot** > **Day summary**, enabled once a parcel is registered, sums up the day's arrivals
  for the next shift with Apple's language model (Foundation Models, macOS 27):
  `Day summary: ...`. A Mac without Apple Intelligence asks Apple's server model instead
  (Private Cloud Compute): `Day summary from Apple's servers: ...`. Without either, the status
  says why: `No day summary: this Mac has no Apple Intelligence`.

`acceptance/features/desk-models/desk-models.feature` holds these to their stubs: models of
Apple's models' contracts, which the `models-macos` pack answers the app's calls from
(`acceptance/models/*-macos`, built by `go generate`), so no scenario asks a real model. Its
scenarios are tagged `@macos-models`, which the profiles of the other builds leave out. The builds are signed without the hardened runtime, which lets axx's library
into them.

## What it keeps

A restart keeps both; a scenario's reset clears both.

- **The day's arrivals** (each one's reference, service level, and whether it is fragile), in
  `arrivals.json` in the desk's data folder, where its toolkit keeps an app's data.
- **The service level registered with last**, in its settings, as its toolkit keeps settings.

| Build | Data folder | Settings |
| --- | --- | --- |
| `appkit`, `swiftui` | `~/Library/Application Support/Depot desk` | `UserDefaults`: `example.parcels.depotdesk.appkit`, `example.parcels.depotdesk.swiftui` |
| `winforms` | `%APPDATA%\Parcels\Depot desk` | the registry, `HKCU\Software\Parcels\Depot desk` |
| `wpf` | `%APPDATA%\Parcels\Depot desk` | `settings.json` in `%LOCALAPPDATA%\Parcels\Depot desk` |
| `winui` | `%APPDATA%\Parcels\Depot desk` | the registry, `HKCU\Software\Parcels\Depot desk` |
| `gtk3`, `gtk4` | `$XDG_DATA_HOME/depot-desk` | `settings.ini` in `$XDG_CONFIG_HOME/depot-desk` |
| `qt` | `QStandardPaths.AppDataLocation` | `QSettings`: `UserDefaults` on macOS, the registry on Windows, an ini file on Linux |
| `swing` | `~/.depot-desk` | `settings.properties` in the same folder |
| `electron` | `app.getPath("userData")` | `settings.json` in the same folder |
| `tauri` | the app's data folder (`app_data_dir`) | `settings.json` in its config folder (`app_config_dir`) |
| `flutter` | `getApplicationSupportDirectory()` | `shared_preferences` |
