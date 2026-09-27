---
title: Compare screenshots
description: Check that pages and elements look as designed, pixel by pixel as Playwright compares them, with screenshots kept per platform, each compared on its own.
---

The `web-screenshots` pack checks that pages look as designed: the page, or one element of it, compared with a screenshot taken before, pixel by pixel, as Playwright does. Small differences of color and anti-aliased edges don't count. It builds on the `web-core` pack, which comes with it: `axx pack add web-screenshots` adds both.

```gherkin
Scenario: The quote page looks as designed
  Given the desktop web app with the following properties:
    | url      | http://localhost:8400/portal |
    | viewport | 1280x800                     |
  And the browser's clock is set to "2026-09-25T09:00:00Z"
  When the "/quote" page of the desktop web app is opened
  Then the page looks like the "quote" screenshot
```

A viewport keeps the size of the page the same wherever it runs, and the browser's clock keeps the time it shows.

## Each platform compares its own

Browsers draw the same page differently on each operating system, fonts first, so a screenshot is compared only on the platform it was taken on. Each engine and platform has its own: `screenshots/quote.chromium-linux.png`, `quote.chromium-darwin.png`. Keep the screenshots of every platform you test on, your Mac and your CI's Linux say, and each of them compares, locally as in CI. `platforms` names them:

```yaml title="axx.yaml"
packs:
  web-screenshots:
    platforms: [linux, darwin]
```

On a platform that is not in the list, the steps pass without comparing and say so; the rest of the scenario runs as usual. Keep only your CI's platform (`platforms: [linux]`) to compare in CI alone. Without `platforms`, every platform compares, and one without its screenshots takes them.

## Taking and updating screenshots

- The first time, there is no screenshot: the step takes it and fails. Look at it, commit it, and the next runs compare with it.
- When the page looks different, the step attaches the screenshot, the page and their difference to the report (red: what differs; yellow: anti-aliasing), and keeps the page in `.axx/web/screenshots/`. When the change is right, copy that over the screenshot, or delete the screenshot and run again.
- `packs.web-screenshots.update: true` takes every screenshot again, as the pages look now, for the platform it runs on: `axx run --set packs.web-screenshots.update=true` on your Mac, and on Linux in the CI job that compares them, keeping them as an artifact. Take a platform's screenshots where they are compared: another Linux draws text with other fonts and smoothing, and Playwright's image (`mcr.microsoft.com/playwright`) does not draw like GitHub's Ubuntu runners.
- The check waits for the page to settle, such as a price that arrives, before it fails.

## One element

`the "testid=quote" element looks like the "express quote" screenshot` compares one element, by its name or a selector: a card, a chart, a label, without the rest of the page around it.

## What changes from run to run

A page that shows the time, a generated number or today's date looks different every run. Paint it over, in both the screenshot and the page, one element a row, by its text, its label or a selector:

```gherkin
Scenario: A parcel's page looks as designed, whatever the time it was registered
  Given the desktop web app with the following properties:
    | url      | http://localhost:8400/portal |
    | viewport | 1280x800                     |
  And a seeds/portal-look.yaml db seed
  When the "/parcels/PX-WEB-5181" page of the desktop web app is opened
  Then the page looks like the "parcel" screenshot, apart from:
    | css=time |
```

What is painted over in either screenshot is left out of the comparison, so an element may change size from run to run, like a time whose digits differ.

Better still, give pages fixed data: a seeded parcel, and the browser's clock set to a time ([The browser's clock](/guides/web-pages/#the-browsers-clock)).

## Settings

| Setting | |
| --- | --- |
| `platforms` | the platforms whose screenshots you keep, and compare on: `linux`, `darwin`, `windows` (every platform by default) |
| `folder` | the folder of the screenshots, in the project (`screenshots` by default) |
| `tolerance` | the share of a screenshot's pixels that may differ, like `0.01` (one pixel in a hundred); keep it at 0 when you can |
| `update` | take every screenshot again instead of comparing |
