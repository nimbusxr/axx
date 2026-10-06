# Depot desk in Electron

The depot desk (`../README.md`) in Electron: the page in `../web`, what it keeps in the app's
`userData` folder (`arrivals.json`, `settings.json`), and the Depot menu.

```sh
npm install
npx electron .
```

`npx electron` fetches Electron's binary as it first starts. axx starts the binary itself
(`node_modules/electron/dist`), so fetch it first: `node node_modules/electron/install.js`.

On macOS, Electron builds its accessibility tree once asked for it (`AXManualAccessibility`, as
VoiceOver asks); on Linux, once started with `--force-renderer-accessibility`.
