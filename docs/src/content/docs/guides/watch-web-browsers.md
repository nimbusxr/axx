---
title: Watch and debug browsers
description: Watch the browsers of a run as they go, pause a scenario in Playwright's Inspector before a step or where it failed, and read the trace, video and screenshot a failed scenario keeps.
---

The browsers run on your machine, and you can see them: live while a run goes, and afterwards, in what a failed scenario keeps.

## Watch a run

With `watch` on, the scenarios' browsers open in windows on your screen as they go, windows you can use as your own: click in them, and open DevTools to look at the page, its console and its network.

```yaml title="axx.yaml"
profiles:
  watch:
    run:
      workers: 1 # one browser at a time
    packs:
      web-core:
        watch: true
        slowdown: 300ms
```

- `axx run --profile watch` runs with it. `axx.local.yaml` can hold the same settings for you alone, and `axx run --set packs.web-core.watch=true` switches it on for one run.
- `slowdown` waits after every action, to follow what happens.
- Watching needs a screen: on a Linux machine without one, such as a CI runner, run without it.

In the IntelliJ plugin and the VS Code extension, *Watch* runs scenarios this way, one at a time, with a slowdown of 300ms unless you set another ([Set up your editor](/guides/set-up-your-editor/#watch-the-browsers)). Watching only shows the browsers: to stop at a step, or where a scenario fails, pause it.

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
  When the "Weight (grams)" field is filled with "1200"
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
- A **video** of each browser tab, when `videos` says so, in `.axx/web/videos/`, and in the report, where it plays.
- The files the browser **downloaded** that its steps checked, in `.axx/web/downloads/`.

Secrets stay out of all of them but the screenshot and the videos, which show the page as it is ([Keep secrets out of reports](/guides/web-sign-in/#keep-secrets-out-of-reports)).

## Settings

What scenarios keep, and how browsers run, is up to `packs.web-core` in `axx.yaml`:

| Setting | |
| --- | --- |
| `traces` | which scenarios keep a trace: `failed` (the default), `always` or `never` |
| `videos` | which scenarios keep a video of each browser tab: `never` (the default), `failed` or `always` |
| `watch` | `true` shows the browsers in windows on your screen instead of running them unseen |
| `slowdown` | a wait after every action, like `300ms` |
| `pauseOnFailure` | pause a failed scenario where it failed, in Playwright's Inspector |
| `scriptErrors` | `report` (the default) lists the pages' script errors with a failed scenario; `fail` fails any scenario whose pages had them |
| `testIdAttribute` | the attribute `testid=` selectors look at (`data-testid` by default) |
