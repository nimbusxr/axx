#!/bin/sh
# Builds the depot desk in Flutter for the OS it runs on, into build/.
set -eu
cd "$(dirname "$0")"
case "$(uname -s)" in
  Darwin) os=macos ;;
  Linux) os=linux ;;
  *) os=windows ;;
esac
# The OS's runner is Flutter's own, made here rather than kept: then the
# window's title, and on macOS no App Sandbox (README.md).
flutter create --platforms="$os" --project-name depot_desk --org example.parcels . >/dev/null
case $os in
  macos)
    sed -i '' 's/^PRODUCT_NAME = .*/PRODUCT_NAME = Depot desk/' macos/Runner/Configs/AppInfo.xcconfig
    for f in macos/Runner/*.entitlements; do
      /usr/libexec/PlistBuddy -c "Set :com.apple.security.app-sandbox false" "$f"
    done
    grep -q setContentSize macos/Runner/MainFlutterWindow.swift ||
      sed -i '' 's/    let windowFrame = self.frame/    self.setContentSize(NSSize())\n    let windowFrame = self.frame/' macos/Runner/MainFlutterWindow.swift
    sed -i '' 's/NSSize([^)]*)/NSSize(width: 640, height: 580)/' macos/Runner/MainFlutterWindow.swift ;;
  windows)
    sed -i 's/L"depot_desk"/L"Depot desk"/' windows/runner/main.cpp
    sed -i 's/Win32Window::Size size([0-9]*, [0-9]*);/Win32Window::Size size(640, 580);/' windows/runner/main.cpp ;;
  linux)
    sed -i 's/"depot_desk"/"Depot desk"/g' linux/runner/my_application.cc
    sed -i 's/gtk_window_set_default_size(window, [0-9]*, [0-9]*);/gtk_window_set_default_size(window, 640, 580);/' linux/runner/my_application.cc ;;
esac
flutter build "$os" --release
