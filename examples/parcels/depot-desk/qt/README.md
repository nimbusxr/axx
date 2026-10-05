# Depot desk in Qt

The depot desk (`../README.md`) in Qt, written with PyQt: Qt 6 by default, Qt 5 with `--qt5`.
Both are Qt's own widgets and Qt's own accessibility, as in an app written in C++.

## Run

With PyQt from pip, on macOS, Windows and Linux:

```sh
python -m venv .venv && .venv/bin/pip install PyQt6 PyQt5
.venv/bin/python desk.py          # Qt 6
.venv/bin/python desk.py --qt5    # Qt 5
```

On Debian and Ubuntu, `apt install python3-pyqt6 python3-pyqt5` installs them too.

## Where Qt differs from the spec

- **Print label** is a checkbox: Qt has no switch.
- **Handover rules** is a link in a label, which accessibility reads as text: Qt has no link.
- **Courier signature** is a label that shows the drawing, so that accessibility has it as an
  image. A plain widget has no role, and macOS leaves it out of the tree; PyQt cannot give a
  widget a role of its own.

## What it keeps

Its arrivals in `arrivals.json` in `QStandardPaths.AppDataLocation`
(`~/Library/Application Support/Parcels/Depot desk` on macOS), and the service level in
`QSettings`: the preferences domain `com.parcels-example.Depot desk` on macOS, the registry key
`HKCU\Software\Parcels\Depot desk` on Windows, `~/.config/Parcels/Depot desk.conf` on Linux.
