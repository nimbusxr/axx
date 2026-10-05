# Depot desk in GTK 4

The depot desk (`../README.md`) in GTK 4, written with PyGObject: GTK's own widgets and GTK's
own accessibility, as in an app written in C.

```sh
sudo apt install python3-gi gir1.2-gtk-4.0    # Debian, Ubuntu
python3 desk.py
```

## What it keeps

Its arrivals in `$XDG_DATA_HOME/depot-desk/arrivals.json` (`~/.local/share` by default), and the
service level in `$XDG_CONFIG_HOME/depot-desk/settings.ini` (`~/.config`).

GTK 4 has no menu bar widget: the Depot menu is the application's menu model, which the window
shows as its menu bar.

## What GTK 4.14 gives accessibility (Ubuntu 24.04)

- Places in the window only: on the screen, everything is at the origin.
- Its popover menus are not in the tree: an open Depot menu cannot be found.
- Its radio buttons and its switch are check boxes, and a drawing area with no role is left
  out: the signature pad has the image role, given as it is made.
- A list's rows are made as they scroll into view, each row's element passed on to the rows
  that take its place.
