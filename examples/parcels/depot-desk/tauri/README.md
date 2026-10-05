# Depot desk in Tauri

The depot desk (`../README.md`) in Tauri 2: the page in `../web` in the system's web view
(WKWebView on macOS, WebView2 on Windows, WebKitGTK on Linux), what it keeps in the app's data
and config folders (`arrivals.json`, `settings.json`), and the Depot menu.

```sh
./build.sh    # Rust and Node.js; on Linux, WebKitGTK's development files
src-tauri/target/release/depot-desk
```

`build.sh` builds the app without a bundle (`tauri build --no-bundle`): the executable as it
ships inside one.
