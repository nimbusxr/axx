---
title: Measure JavaScript coverage
description: Find out how much of the web apps' JavaScript the scenarios run, as lcov and Istanbul reports for editors, CI and coverage services.
---

The `web-coverage` pack collects the coverage of the scripts the web apps' pages load while the scenarios run, and writes it when the run ends. It has no steps: add it (`axx pack add web-coverage`, which brings the `web-core` pack it builds on), and every scenario's pages count.

```text
the web apps' JavaScript coverage: lines 90.32%, statements 90.32%, functions 100%, branches 89.47%, in .axx/web/coverage/lcov.info
```

## What counts

The scripts the pages load from their web app's origin, named by their URL path, like `portal/js/tracking.js`. A script with a source map counts as its original files instead: the TypeScript, not the bundle. Scripts from other origins (a chat widget, analytics), inline scripts and event handlers in the HTML do not count.

## Name the project's files

`sources` says where the scripts are in the project, by the start of their URL path, so that the reports name the files editors and coverage services know:

```yaml title="axx.yaml"
packs:
  web-coverage:
    sources:
      /portal/js/: ../app/web/js      # the scripts the portal serves under /portal/js/
      /portal/src/: ../app/web/src    # the original files of their source maps
```

With `sources`, only the scripts under those paths count, named by the folder and the rest of the path, relative to the project: `../app/web/js/tracking.js`. When several paths match, the longest wins. A source map that lacks its original files' content finds them there too.

## The reports

When the run ends, the pack writes to `.axx/web/coverage` (`folder`):

| File | For |
| --- | --- |
| `lcov.info` | editors' coverage gutters, `genhtml`, and coverage services (Codecov, Coveralls, SonarQube) |
| `coverage-final.json` | Istanbul's own format: `npx nyc report --reporter=html` makes an HTML report of it |
| `coverage-summary.json` | the totals, for CI thresholds and badges |

The run prints the totals at the end.

```text title="lcov.info, the start"
SF:../app/web/js/tracking.js
FN:3,$
FN:5,day
FNDA:16,$
FNDA:6,day
DA:11,6
DA:12,0
```

A line that ran 0 times is one no scenario reached: in the parcels example, the tracking page's message for an estimate it cannot fetch.

## How it counts

The pack uses Chromium's own coverage: the lines, functions and branches that ran, and how many times. It takes it after each step, and before a page leaves its document for another, and adds up every page of every scenario. The reports are those Node's tools (c8) write from the same coverage, byte for byte.

It works in Chromium, Chrome and Edge. A web app in Firefox or WebKit runs without coverage, and the run says so, once for each web app. Not counted: web workers, frames from other origins, and what runs while a tab a page opened is still loading. Pages run a little slower while the pack collects.

## Settings

| Setting | |
| --- | --- |
| `folder` | where the reports go, in the project (default `.axx/web/coverage`) |
| `sources` | where the scripts are in the project, by the start of their URL path |
