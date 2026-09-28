---
title: Audit pages with Lighthouse
description: Check a page's Lighthouse scores for performance, accessibility, best practices and SEO, and how fast it loads by the Core Web Vitals.
---

The `web-lighthouse` pack audits a web app's pages with [Lighthouse](https://developer.chrome.com/docs/lighthouse), Google's tool for the quality of web pages, in the browser the scenario already uses. It builds on the `web-core` pack, which comes with it: `axx pack add web-lighthouse` adds both.

```gherkin
Scenario: The quote page is quick and sound on a phone
  When the "/quote" page is opened
  Then the "/quote" page scores at least:
    | performance    | 90  |
    | accessibility  | 100 |
    | best practices | 100 |
    | seo            | 100 |
  And the "/quote" page loads within:
    | largest contentful paint | 2.5s  |
    | total blocking time      | 200ms |
    | cumulative layout shift  | 0.1   |
```

The page is a path below the web app's `url`, or a whole URL. A page of the web app must be open first: the audit runs beside it.

## Scores

`the {string} page scores at least:` checks the page's Lighthouse scores, from 0 to 100: `performance`, `accessibility`, `best practices` and `seo`, a row each, with the lowest score each may have. Lighthouse audits the categories listed only. A score below its minimum fails the step, which lists the audits that cost the page the most:

```text
The "/parcels/new" page scores below what it should:
  performance: 72, at least 90
    Total Blocking Time: 1,150 ms
    Cumulative Layout Shift: 0.135
    Use efficient cache lifetimes: Est savings of 97 KiB
    Improve image delivery: Est savings of 80 KiB
    Layout shift culprits
    and 2 more in Lighthouse's report
  (accessibility: 100, best practices: 100, seo: 100)
```

## Loading

`the {string} page loads within:` checks how fast the page loads, a metric a row: the Core Web Vitals `largest contentful paint` and `cumulative layout shift`, and `first contentful paint`, `total blocking time` and `speed index`. A limit is a duration (`2.5s`, `200ms`), except for the layout shift, which has no unit (`0.1`). A metric over its limit fails the step, with what would save the most on it. Google's limits for a good page are 2.5 s for the largest contentful paint, 200 ms of blocking time and a shift of 0.1.

## How it audits

- **Beside the scenario.** Lighthouse loads the page afresh in a tab of its own, with the web app's cookies and storage: a signed-in page is audited signed in. The scenario's steps go on in their tab.
- **As a first visit.** Lighthouse clears the browser's cache and the site's service workers first, for the scenario's tabs too.
- **On a phone, or a desktop.** It measures the page as a mid-range phone on a slow 4G connection loads it (the `device` setting: `desktop` for Lighthouse's desktop preset), simulating the device and the connection rather than slowing the browser down. An audit takes about 5 seconds, and audits take turns in a run, so that scenarios running beside one another do not slow each other's pages.
- **With a report.** Each audit attaches Lighthouse's HTML report, which the pack also keeps in `.axx/web/lighthouse`.

Scores and timings vary a little from run to run, and from machine to machine: leave room in the limits. The blocking time is time the browser spends on the CPU, which other scenarios' browsers share: on a busy machine, such as a CI runner, run the scenarios that time pages alone, after the others, by tagging them and listing the tag in `run.exclusive` ([Run in parallel](/guides/parallel-runs/)). Lighthouse audits pages in Chromium, Chrome and Edge only; for a web app in Firefox or WebKit the step fails.

## Settings

| Setting | |
| --- | --- |
| `device` | `mobile` (the default): a mid-range phone on a slow 4G connection; or `desktop`: Lighthouse's desktop preset |

Lighthouse is [Apache-2.0](https://github.com/GoogleChrome/lighthouse/blob/main/LICENSE), by Google: Axx downloads Lighthouse 13.5.0 the first time (about 18 MB, from npm's registry, or the mirror `npm_config_registry` names) and runs it with the web-core pack's Node.js. It does not ship it.
