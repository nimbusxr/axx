---
title: How the web-core pack works
description: How Axx drives real browsers - Playwright's browsers on your machine, a browser context for every scenario, steps that wait and find things the way people do - and why it works that way.
---

The `web-core` pack runs acceptance scenarios in real browsers. It stands on [Playwright](https://playwright.dev), the browser automation Microsoft maintains for Chromium, Firefox and WebKit, and it keeps to what Axx is for: scenarios people can read, black-box tests of what your service does ([Black-box testing](/explanations/black-box-testing/)).

## The browsers run on your machine

The browsers run where Axx runs, as they do for any Playwright project: Playwright's builds of Chromium, Firefox and WebKit, which the pack downloads the first time it uses each one, or the Google Chrome and Microsoft Edge installed on the machine. Axx drives them through the Playwright driver, Node.js and Playwright's own package, which it downloads once and checks against pinned hashes. So:

- The browsers reach your web apps at the addresses you would: `localhost`, whether their service runs in Compose or on your machine, under a debugger or not.
- Watching a run is watching browser windows on your screen, with their DevTools, as when you use the app yourself.
- Scenarios run the same everywhere: the pack pins one Playwright version, so its browsers are the same builds on every laptop and in CI, and it gives the browser a language and a time zone (`en-US` and `UTC`) unless the web app says otherwise, rather than the machine's.

## Playwright's tools, in steps

Playwright's own tools debug the scenarios: its Inspector, where a run pauses, its recorder and its trace viewer. They speak Playwright's languages, and the pack teaches them the steps' instead: the Playwright driver it downloads is patched, for the Playwright version it pins, so that

- the Inspector shows the feature file, at the step a paused scenario waits before, and every browser action belongs to its step;
- picking an element names it as steps do, `the "Weight (grams)" field`, and the Inspector finds what such a name finds;
- the recorder writes the steps people take, with the steps of the pack;
- a trace groups the browser's actions by step, keeps the feature file, and names elements as steps do in the trace viewer your editor opens.

What people see names what steps find: an element that the recorder can only find by its position or its markup has no name for people either, and the recording says so.

## Isolation

Playwright isolates tests in **browser contexts**: a context is a fresh browser profile, with its own cookies, storage, cache, permissions and pages, and nothing of one is visible to another. The pack keeps one browser per engine for the whole run, and gives every scenario a context of its own, for each web app it registers. Scenarios run side by side without seeing each other's sessions, and a context is gone when its scenario ends ([Scenario isolation](/explanations/scenario-isolation/)).

That is also why the pack has no way to sign in once and share the session with every scenario: shared, one scenario signing out would sign out the others, and scenarios would depend on each other again. A scenario signs in, or starts with its own copy of a session ([Sign in to web apps](/guides/web-sign-in/)).

## The current page

A scenario acts on one page at a time, the current page, as a person looks at one tab. Opening a page makes it current; a link that opens a new tab makes the new tab current, as browsers show it; closing it goes back to the tab it came from. With several web apps, the one whose page was opened last is current.

A dialog the page opens blocks the page, for Axx as for people: until a step answers it, other steps fail and say so, rather than hang or go on behind it.

## Waiting instead of sleeping

Pages change after the fact: a price arrives, a message goes, a button is enabled once the form is valid. Every action waits for its element to be visible, enabled and still; every check retries until the page shows what it expects, 10 seconds or `within {duration}`. A step that has to wait longer than that says what the page shows instead, and a scenario never needs a pause.

## Names first, selectors when needed

Steps find things the way people do: a field by its label, a button by its name, text anywhere. This keeps scenarios readable to the people who own the acceptance criteria, keeps them working when the markup changes, and makes accessibility part of the test: a field a step cannot find by its label is one a screen reader cannot name either. Steps look inside frames the same way, since people see no frames.

Selectors (`css=`, `xpath=`, `testid=`) go where names are not enough: rows of a list, a canvas, a widget of a library with no accessible names. They take the name's place in the same steps, so there is one set of steps to know.

## Secrets

Values a step takes from the environment, `${env:…}`, are secrets: passwords, tokens, session cookies. They are masked in failure messages, in the failure report, and in saved traces, where they would otherwise sit in the recorded actions, the snapshots of the page, and the form data and headers of its requests. Screenshots and videos show the page as it is.

## Packs that build on it

The `web-core` pack drives the browsers and holds the steps every web app needs. What only some projects need are packs of their own, which build on it as `aws-s3` builds on `aws-core`: a project adds the ones it uses ([Choose packs](/guides/use-packs/)).

| Pack | |
| --- | --- |
| `web-screenshots` | pages that look as designed, each platform with its own screenshots ([Compare screenshots](/guides/web-screenshots/)) |
| `web-a11y` | WCAG audits with axe-core, and what a screen reader reads ([Check accessibility](/guides/web-accessibility/)) |
| `web-network` | what the pages fetch: requests that fail, answer or take their time, recordings, websockets ([Control what pages fetch](/guides/web-network/)) |
| `web-lighthouse` | Lighthouse scores and the Core Web Vitals ([Audit pages with Lighthouse](/guides/web-lighthouse/)) |
| `web-coverage` | how much of the web apps' JavaScript the scenarios run ([Measure JavaScript coverage](/guides/web-coverage/)) |

They use the pages the `web-core` pack opens, found the way its steps find things: a screenshot of `the "Price" element` is of the element the other steps name so. The tools some of them bring (axe-core, Lighthouse) are downloaded the first time, as the browsers are.

## What Playwright cannot do

A few limits are Playwright's (or the browsers') and hold in the pack too:

- Firefox has no mobile emulation: a `device` that is a phone needs `chromium` or `webkit`.
- WebKit shows some downloads in the page, text files among them, unless their link has the `download` attribute.
- Pages learn the browser's location on secure addresses only (`https://`, or `localhost`).
- Google Chrome and Microsoft Edge exist for x86-64 Linux only; on ARM Linux machines, use Chromium, Firefox and WebKit.
