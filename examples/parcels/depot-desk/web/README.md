# The depot desk's page

The depot desk (`../README.md`) as one web page, which the Electron and Tauri builds show. It
keeps nothing itself: `window.depot`, which the app around it gives (Electron's preload; under
Tauri, its commands), loads and saves the arrivals and the service level, and turns the Depot
menu's Close day on and off.

What the page does for accessibility, and why:

- The switch is a button with `role="switch"`, and its look is a shape: text that CSS
  generates (`::before`) joins an element's accessible name.
- The arrivals list has `role="list"`: WebKit drops a list's role when its style drops its
  bullets.
- The signature pad is a canvas with `role="img"` and a label.
- The parcels expected today are on the page before the day the app kept loads: WebKitGTK
  showed assistive technology none of the rows added to a table it had already read, and on a
  slow machine it read the page before the day came.
