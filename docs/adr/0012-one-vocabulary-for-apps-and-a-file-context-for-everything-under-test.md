# 0012: Every app is driven with the same steps, and everything under test has a file context

- Status: Accepted
- Date: 2026-10-05
- Amends: [0011](0011-desktop-apps-through-the-accessibility-tree.md)

## Context

The mobile packs (`mobile-core`, `mobile-ios`, `mobile-android`) drive an app with steps that
say "the courier app". ADR 0011 gave desktop apps the same steps, worded "the depot desktop
app" so the two sets would not clash in one project. That gives the same actions (launch an
app, tap or click a control, fill a field, check what it shows) two vocabularies, which differ
only by a word bolted on to keep step text unique. The registrations disagree too: mobile has a
step per platform (`the courier android app with…`), desktop one step with a row per OS.

`apps` in `axx.yaml` names something else: the services axx starts and stops around a run (the
system under test and its infrastructure, often one `docker compose up`). So "app" means two
things.

Files have no common shape. The files pack reads a folder by its path on the machine, relative
to `axx.yaml` (`../infra/exports` in the parcels example, the host side of a Compose bind
mount). A file an app saves has no such path: a desktop app's home is made by axx for each
scenario, under `.axx`, which no one is meant to name; a phone app's files are in its sandbox on
its device. ADR 0011 proposed exposing a desktop app's home as a value of the scenario
(`${desktop:snap.home}`), which would have put axx's own folders, and on Linux which desktop a
scenario ran on, into features.

## Decision

### One vocabulary for every app

- **Packs**: `app-core` has the steps every app takes, worded once, and the registry of apps.
  `mobile-core` (devices, Appium) is under it, with `mobile-ios` and `mobile-android`;
  `desktop-core` (the reset, taking turns at the desktop) is under it, with `desktop-macos`,
  `desktop-windows` and `desktop-linux`. Adding a platform's pack brings its family's core and
  `app-core` with it. The word "desktop app" goes from every step.
- **`app-core`'s steps** (each check also as "within {duration} ..."):

  ```gherkin
  When the {word} app is launched
  When the {word} app is restarted
  When the {string} {control} is tapped in the {word} app
  When the {string} {control} is clicked in the {word} app
  When the {string} field in the {word} app is filled with {string}
  When the {string} {control} is scrolled into view in the {word} app
  Then the {word} app shows {string}
  Then the {word} app does not show {string}
  Then the {string} {control} is shown in the {word} app
  Then the {string} {control} is enabled in the {word} app
  Then the {string} {control} is disabled in the {word} app
  Then the {string} field in the {word} app has the value {string}
  Then the {word} app looks like the {string} screenshot
  ```

  `tapped` and `clicked` are one step, and either word works on every platform: the platform
  decides how the control is pressed (a touch on a phone, the pointer on a desktop). The docs
  use "tapped" for phones and "clicked" for desktops.
- **`{control}`** is every kind either family finds: `button`, `field`, `checkbox`, `radio
  button`, `switch`, `tab`, `menu`, `menu item`, `list item`, `row`, `link`, `image`, `text`
  and `element`, and their plurals. A kind a platform does not have (a phone has no menu bar)
  fails the step, naming the kinds it has.
- **A family's own steps** stay in its core, worded the same way: `mobile-core` has dialogs,
  notifications, opening a link, the background and swipes; `desktop-core` has keys
  (`the {word} key is pressed in the {word} app`), clicks at a position and drags;
  `mobile-android` has the back button.

### Registration per platform

- Each platform's pack registers an app on it, with that platform's properties:

  ```gherkin
  Given the courier android app with the following properties:
  Given the courier ios app with the following properties:
  Given the depot macos app with the following properties:
  Given the depot windows app with the following properties:
  Given the depot linux app with the following properties:
  ```

  A desktop app is `app` (a `.app`, a bundle identifier, an `.exe`, a packaged app's ID, an
  executable), with `args`, `env.<name>`, `locale` and `timezone`; `registry` is Windows' own.
  The mobile packs' properties are unchanged.
- **One platform per app in a scenario.** A scenario and its Background share the
  registrations, and one app registered for two platforms the machine can run (Android and iOS
  on a Mac) is an error naming both. A registration for a platform the machine cannot run
  (Windows, on a Mac) does nothing there, so a feature lists a desktop app once for each OS and
  runs unchanged on each. A machine with no registration of an app it can run fails the app's
  first step, naming the registration to add.
- **Different apps may run on different platforms** in one scenario: the courier's Android app
  confirms a delivery, and the depot's macOS desk shows it arrived. axx claims a scenario's
  devices and the desktop in a fixed order, so two scenarios never each hold one and wait for
  the other.

### `apps` in `axx.yaml` becomes `services`

What axx starts and stops around a run is `services`, and everything named after it follows:
`services.<name>.dir`, `.command`, `.ready`, `.dependsOn` and the rest, doctor's checks and
hints, `axx env` and `axx up` (whose JSON says `"services"`), the MCP server's env tool, the
JSON Schema and the docs. "App" then means only an app under test. The JSON change is a change
to the CLI contract (ADR 0003), which v0.x allows; the release notes name it.

### Everything under test has a file context

- **A folder says whose it is.** The files pack's folder registration takes `owner`, a kind
  and a name as axx's references write them (`${env:NAME}`): `service:parcels` for a service
  in `axx.yaml`, `app:depot` for a registered app. `path` is then relative to that owner's
  file context:

  ```gherkin
  Given the exports folder with the following properties:
    | owner | service:parcels |
    | path  | ./exports       |
  ```

  The kind is required: a bare name fails, naming the two forms, so a service and an app of
  one name never mix. With no `owner`, a folder is on the machine, relative to `axx.yaml` or
  absolute, as now. Examples and docs write paths relative to an owner with `./`.
- **A service's context** is where axx runs it: its `dir` in `axx.yaml`. A path is relative to
  it, or absolute. axx does not know how a service is started: a service in a container is
  checked where its files land on the machine (a bind mount of the project's), never inside
  the container.
- **An app's context** is made by axx for each scenario, wherever the app runs. `./` is where
  the platform keeps an app's data, so a desktop app built once has one path on every OS; `~/`
  is the app's home, for apps that keep their files there:

  | Platform | `./` | `~/` | axx reads the files |
  | --- | --- | --- | --- |
  | macOS | `Library/Application Support` in the app's home | the app's home | directly |
  | Windows | `AppData\Roaming` in the app's home | the app's home | directly |
  | Linux | `.local/share` in the app's home | the app's home | in the app's desktop |
  | iOS (simulator) | the app's sandbox | the app's sandbox | through `simctl get_app_container` |
  | Android | the app's data folder | the app's data folder | `adb run-as` (debuggable builds); shared storage with `adb pull` |
  | A device farm (Appium) | the app's files | the app's files | Appium's `pullFile` |

- **A path with spaces is quoted**, in the file checks and wherever a step takes a
  `{filepath}`: `the "Parcels/Depot desk/arrivals.json" file in the desk folder contains
  "PX-DSK-4102"`. Unquoted paths keep working. Spaces are not escaped with a backslash, which is
  Windows' path separator.
- **axx resets only what it makes**: an app's home on a desktop, an app's data on its device,
  before each scenario. It never empties a project's or a service's folders: that is the
  project's to do, if it wants to (the parcels example's Compose file does). Whether a file must
  be unique to a scenario depends on the test: a report a service writes once for the run,
  checked by its rows, is shared by every scenario.

### Linux desktops are invisible

Each Linux desktop axx runs is a box of its own, like an emulator: its screen (Xvfb), its
session and accessibility buses, and its own view of the file system (a mount namespace, made
from Go without cgo), in which an app's home is at the same path in every desktop. A feature
never names a desktop or a folder of axx's: an app's files are reached through the app, as
above. `packs.desktop-linux.desktops` says how many Linux desktop scenarios run at once, as
`packs.mobile-android.devices` does for Android.

## Consequences

- One set of steps for every app: a person or an agent who has tested a phone app can test a
  desktop app, and the reference lists each step once.
- Mobile's steps keep their wording and move to `app-core`; "clicked" joins "tapped", and the
  kinds of `{control}` grow. Projects using the mobile packs change nothing in their features
  (the platform packs bring `app-core` with them). `axx.yaml`'s `apps` becomes `services`, which
  every project that starts services changes.
- ADR 0011's approved desktop steps are replaced by `app-core`'s and `desktop-core`'s, and its
  registration by one per OS. Its scenario value for an app's home is withdrawn: file contexts
  take its place.
- An app's files are checked with the files pack's checks, unchanged: a file's content, its JSON
  properties, its text, a row of its table.
- Android's private files can be read only in debuggable builds; a release build's are not
  reachable, and the step says so.
- Linux desktops need user and mount namespaces, which CI runners and container hosts have; a
  host that forbids them runs one Linux desktop scenario at a time, and `axx doctor` says why.
