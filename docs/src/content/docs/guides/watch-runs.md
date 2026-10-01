---
title: Watch a run
description: See what scenarios do as they do it, web and mobile alike, with browsers and devices in windows on your screen, one scenario at a time, slowed down to follow.
---

A run you watch shows what its scenarios do as they do it. Browsers open in windows on your screen, an iOS simulator in Device Hub, and an Android emulator in its own window. One switch covers every kind of app a scenario uses:

```sh
axx run --watch                      # one scenario at a time, on screen
axx run --watch --slowdown 500ms     # with a pause after every action, to follow it
```

- `--watch` runs one scenario at a time, unless `--workers` (or `run.workers`) says otherwise.
- `--slowdown` pauses after every action: a click, a tap, a typed field, a launch.
- Watching needs a screen. On a machine without one, like a CI runner, run without it.

The same settings work as `run.watch` and `run.slowdown` in `axx.yaml`. A profile keeps them for when you want them:

```yaml title="axx.yaml"
profiles:
  watch:
    run:
      watch: true
      slowdown: 300ms
```

`axx run --profile watch` runs with it, and with another profile after a comma, like `--profile ios,watch`, it watches that profile's run ([Select by what scenarios use](/guides/tags-and-filtering/#select-by-what-scenarios-use)). `axx.local.yaml` can hold the settings for you alone. In your editor, *Watch* (next to *Run* and *Debug*) runs a scenario this way ([Set up your editor](/guides/set-up-your-editor/#watch-a-run)).

## What you see

| App | While you watch |
| --- | --- |
| Web ([`web-core`](/guides/test-web-apps/)) | Each browser in a window you can use as your own: click in it, and open DevTools on its page, console and network. `packs.web-core.watch` shows the browsers alone ([Watch and debug browsers](/guides/watch-web-browsers/)). |
| iOS ([`mobile-ios`](/guides/test-mobile-apps/)) | The simulator in Device Hub (the Simulator app before Xcode 27), opened on it before the scenario starts. The simulator is in Xcode's own set, named like `axx iPhone 17, iOS 27.0 (1)`, and stays booted after the run, so the next watched run starts at once ([keep](/guides/test-mobile-apps/#keep-simulators-warm)). |
| Android ([`mobile-android`](/guides/test-mobile-apps/)) | The emulator in its window. |

Each scenario still has its device to itself, and its app reset. Watching changes what you see, not where a scenario starts from.

## Stop where it matters

Watching doesn't pause. To stop a web scenario before a step, or where it fails, pause it in Playwright's Inspector ([Watch and debug browsers](/guides/watch-web-browsers/#pause-a-scenario)). What a failed scenario keeps (screenshots, traces, videos, and the page or screen it was on) is in the [report](/guides/reports/).
