# Depot desk in SwiftUI

The depot desk (`../README.md`) in SwiftUI, a single Swift file built into an app without an
Xcode project.

## Build

```sh
./build.sh
open "build/Depot desk.app"
```

`build.sh` compiles `main.swift` with Xcode's Swift, makes `build/Depot desk.app` around it, and
signs it ad hoc. It needs Xcode, or its command line tools.

## Where SwiftUI differs from the spec

- **Arrivals** is a table of one column, as lists are on macOS: its arrivals are rows.

## What it keeps

Its arrivals in `~/Library/Application Support/Depot desk/arrivals.json`, and the service level
in `UserDefaults`, the preferences domain `example.parcels.depotdesk.swiftui`.
