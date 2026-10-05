# Depot desk in Swing

The depot desk (`../README.md`) in Java's Swing, one source file built into a jar.

## Build and run

```sh
./build.sh                       # needs a JDK, 17 or later
java -jar build/depot-desk.jar
```

## Where Swing differs from the spec

- **Print label** is a checkbox: Swing has no switch.
- **Handover rules** is a label people click, with the hyperlink role: Swing has no link. On
  macOS, accessibility reads it as text.
- **Courier signature** is a label showing the drawing as its icon. macOS leaves a component
  that draws itself out of the tree, whatever its role; a label it keeps, as an image.
- On macOS, Java gives a tab group's tabs as elements that are gone as they are read. They are
  found where they are drawn, along the top of the tab group, as a person finds them.
- Java has hidden components in the tree, of no size: they are not shown.

On Linux, through java-atk-wrapper:

- What is typed into a field is not reported: its text reads as empty.
- A list's items and a table's cells are their renderers' roles (labels), the hyperlink role
  is unknown, and the frame says nothing of its place.
- A menu's items give their places in their popup, not on the screen.
- Java gives the keyboard to a window of its own when clicked: another taking it (as a window
  manager would) loses its keys.

On Windows, through the Java Access Bridge:

- A table's children, and its cells through the bridge's table API, are all the table's one
  cell renderer: a label holding whatever cell was drawn last, with no place. The table's
  visible children are its cells, each with its own text and place.

## What it keeps

Its arrivals in `~/.depot-desk/arrivals.json`, and the service level in
`~/.depot-desk/settings.properties`, both in the home Java reads (`user.home`). Not in
`java.util.prefs`: on macOS, those are every Java app's, in one preferences domain (an IDE's
among them), which no reset can empty for one app.
