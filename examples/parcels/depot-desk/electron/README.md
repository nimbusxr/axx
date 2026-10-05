# Depot desk in Electron

The depot desk (`../README.md`) in Electron: the page in `../web`, what it keeps in the app's
`userData` folder (`arrivals.json`, `settings.json`), and the Depot menu.

```sh
npm install
npx electron .
```

On macOS, Electron builds its accessibility tree once asked for it (`AXManualAccessibility`, as
VoiceOver asks); on Linux, once started with `--force-renderer-accessibility`.
