#!/bin/sh
# Builds the depot desk in Tauri: src-tauri/target/release/depot-desk (.exe on
# Windows). Needs Rust and Node.js; on Linux, WebKitGTK's development files.
set -eu
cd "$(dirname "$0")"
npm install --no-audit --no-fund
npx tauri build --no-bundle
