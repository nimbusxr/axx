---
title: Watch and debug browsers
description: Watch the browsers of a run as they go, pause a scenario in Playwright's Inspector before a step or where it failed, and read the trace, video and screenshot a failed scenario keeps.
---

The browsers run on your machine, and you can see them: live while a run goes, and afterwards, in what a failed scenario keeps.

## Watch a run

`axx run --watch` shows what the scenarios do: their browsers open in windows on your screen as they go, one scenario at a time, and their devices too ([Watch a run](/guides/watch-runs/)). The windows are yours to use: click in them, and open DevTools to look at the page, its console and its network.

```sh
axx run --watch --slowdown 300ms   # wait after every action, to follow what happens
```

- `packs.web-core.watch: true` shows the browsers alone, and `packs.web-core.slowdown` sets their own pace.
- Watching needs a screen: on a Linux machine without one, such as a CI runner, run without it.

In the IntelliJ plugin and the VS Code extension, *Watch* runs scenarios this way, one at a time, with a slowdown of 300ms unless you set another ([Set up your editor](/guides/set-up-your-editor/#watch-a-run)). Watching only shows the browsers: to stop at a step, or where a scenario fails, pause it.

## Pause a scenario

`--pause-at` pauses a run before a step, the line of a feature file; in your editor, a breakpoint on the step's line and *Debug* do:

```sh
axx run features/shop-portal.feature --pause-at features/shop-portal.feature:24
```

The browser shows, and Playwright's Inspector opens next to it, with the feature file at the step:

- **Resume**, or **step over** the browser's actions one at a time.
- **Pick** an element on the page: the Inspector names it as steps do, `the "Weight (grams)" field`, `the "Get a quote" button`. Type a name in its locator box to see what it finds on the page.
- **Record**: use the page, and the Inspector writes the steps you take, ready to copy into the scenario:

  ```gherkin
  When the "Weight (grams)" field is filled with '"1200"'
  And "Germany" is chosen in the "Destination country" field
  And the "Get a quote" button is clicked
  Then the "testid=quote" element shows "6.90 EUR"
  ```

  Where people see no name for an element, no step can find it: the recording says so, and a label, a name or a test id fixes it.
- The browser is yours meanwhile: click in it, open DevTools.

A run that pauses has no timeouts, however long you look. With `packs.web-core.pauseOnFailure: true` (`axx run --set packs.web-core.pauseOnFailure=true` for one run), a failed scenario pauses the same way, at the step that failed: look at the page as the failure left it. The run says which scenario waits for you. Your editor's *Debug* does both: it pauses at your breakpoints and where a scenario fails. The Inspector is a Chromium window, whatever the engine: the pack downloads Playwright's Chromium for it when a run in another engine pauses.

## What a failed scenario keeps

- The failed step attaches a screenshot of the page; the [HTML report](/guides/reports/) shows it with the step.
- The failure report says which page each web app was on, and the dialog it shows (the `context` of `axx run --json`), and lists the pages' script errors.
- A Playwright **trace** of each web app, in `.axx/web/traces/` and attached to the report: the scenario step by step, each step's browser actions under it with a snapshot of the page before and after them, the console and the network. The trace keeps the feature file: its Source tab shows the step. Your editor opens it in Playwright's trace viewer, where elements have the names steps give them; `npx playwright show-trace <file>` and [trace.playwright.dev](https://trace.playwright.dev) open it too, with Playwright's names.
- A **video** of the scenario, when `videos` says so, in `.axx/web/videos/`, and in the report. [Videos](#videos) says what it shows.
- The files the browser **downloaded** that its steps checked, in `.axx/web/downloads/`.

Secrets stay out of all of them but the screenshot and the videos, which show the page as it is ([Keep secrets out of reports](/guides/web-sign-in/#keep-secrets-out-of-reports)).

## Videos

A **video** is the scenario's browser as it ran, with the scenario's steps beside it. Turn videos on with `videos` (`axx run --set packs.web-core.videos=always` for one run).

- **The page:** the tab the scenario acts on. When a link opens another tab, or a step acts on another web app, the video shows that one.
- **The pointer:** drawn where a step clicked, typed or moved it, with a mark where it pressed. Nothing is added to the page itself.
- **The steps:** each one to run, running, passed, failed (its error under it) or skipped, and the lines the running step logs. So what happens off the page shows too: a call to a service, a row in a database.

A video opens on its first frame for a moment, with the steps from the first, and its last frame shows for two seconds, the steps as they ended. It is an MP4 file (H.264) that plays wherever videos play, with a chapter for each step. A failed scenario's video is attached to the report too.

The run keeps one more, `run.mp4`: its kept scenarios one after another, each after a card with its name and whether it passed, and a chapter for each. To post it on a pull request, upload it from CI as any file a job keeps.

The first tab of each web app is recorded at up to 1280 by 720. A tab that a page opens is recorded smaller, and shown at the same size.

Videos are encoded with Cisco's OpenH264. OpenH264 Video Codec provided by Cisco Systems, Inc. The first time a scenario keeps a video, axx downloads Cisco's build for your OS from Cisco (about half a megabyte), checks it, and keeps it in its cache; `videos: never`, the default, turns it off, and axx then downloads nothing. Cisco's terms are in axx's `THIRD_PARTY_NOTICES.md`.

## Settings

What scenarios keep, and how browsers run, is up to `packs.web-core` in `axx.yaml`:

| Setting | |
| --- | --- |
| `traces` | which scenarios keep a trace: `failed` (the default), `always` or `never` |
| `videos` | which scenarios keep a [video](#videos): `never` (the default), `failed` or `always` |
| `watch` | `true` shows the browsers in windows on your screen instead of running them unseen |
| `slowdown` | a wait after every action, like `300ms` |
| `pauseOnFailure` | pause a failed scenario where it failed, in Playwright's Inspector |
| `scriptErrors` | `report` (the default) lists the pages' script errors with a failed scenario; `fail` fails any scenario whose pages had them |
| `testIdAttribute` | the attribute `testid=` selectors look at (`data-testid` by default) |
