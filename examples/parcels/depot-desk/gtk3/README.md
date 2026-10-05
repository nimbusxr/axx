# Depot desk in GTK 3

The depot desk (`../README.md`) in GTK 3, written with PyGObject: GTK's own widgets and GTK's
own accessibility, as in an app written in C.

```sh
sudo apt install python3-gi gir1.2-gtk-3.0    # Debian, Ubuntu
python3 desk.py
```

## What it keeps

Its arrivals in `$XDG_DATA_HOME/depot-desk/arrivals.json` (`~/.local/share` by default), and the
service level in `$XDG_CONFIG_HOME/depot-desk/settings.ini` (`~/.config`).

## What GTK 3 gives accessibility

Its link button is a push button. A tree view's cells are its rows' only elements, made anew
as it scrolls; a cell not laid out has the least integer for its place.
