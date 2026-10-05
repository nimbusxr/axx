# Depot desk in Flutter

The depot desk (`../README.md`) in Flutter, for macOS, Windows and Linux.

```sh
./build.sh    # Flutter 3.47; on Windows, Visual Studio's C++ tools; on Linux, GTK's development files
```

`build.sh` makes the OS's runner with `flutter create` (it is Flutter's own, so it is not kept
here), names the window, and builds the release into `build/`. On macOS it turns the App Sandbox
off: a sandboxed app keeps its data in its container, which axx's reset has yet to prove it
empties.

## What Flutter gives accessibility on macOS

Fewer roles than the other toolkits, which the baseline records:

- The text field has no name, labelled or not: it is the window's only text field.
- Buttons say they are enabled even when they are not.
- Tabs are texts, with their place (`Tab 1 of 2`) on a second line; the arrivals are buttons,
  and the expected parcels' rows are groups of texts.
- Texts in one column are joined into one label, a line each.
- What is scrolled out of its area is squashed onto the area's edge, a point high.

## On Windows

- Flutter's engine serves only MSAA, Windows' older accessibility: its UI Automation is off by
  default. UI Automation reads it through Windows' own bridge, which gives no Invoke, Select or
  Scroll Into View; its default action is Flutter's tap.
- Controls never say they are disabled; the switch is a check box; tabs and rows are texts.
- The tree never follows a scroll
  ([flutter/flutter#189124](https://github.com/flutter/flutter/issues/189124), open): the wheel
  scrolls a list, but each control in it keeps the place it had before, so once a view has
  scrolled, no place in it is true. A control out of view is not scrolled to: it takes its
  default action.

## On Linux

- No control has a place, and asking for one hangs: Flutter is told apart by its embedder,
  loaded in its process, and never asked. Controls take their actions (tap, focus) instead
  of clicks; a disabled control has no actions.
- Menu items are panels.

## What it keeps

Its arrivals in `arrivals.json` in `getApplicationSupportDirectory()`, and the service level
with `shared_preferences`: `UserDefaults` on macOS (`example.parcels.depotDesk`), a file in the
app's data folder on Windows and Linux.
