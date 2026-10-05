---
title: Test web apps
description: Use your web app in real browsers the way people do (Chromium, Firefox, WebKit, Chrome or Edge, on a desktop or a phone), driven by Playwright on your machine, and check in the same scenario what your service did.
---

Many acceptance criteria describe what a person does on a page: a shop fills in a form, clicks a button and sees its parcel registered. The `web-core` pack does exactly that in real browsers, driven by [Playwright](https://playwright.dev). A scenario can use every other pack too, so it also checks what your service did with the form: the row it stored, the event it published.

```gherkin
Scenario: A parcel registered in the portal is stored and announced
  When the "/parcels/new" page is opened
  And the "Shop account" field is filled with "fjord-outdoor"
  And the "Parcel reference" field is filled with "PX-WEB-5001"
  And the "Weight (grams)" field is filled with "1200"
  And the "Recipient name" field is filled with "Anna Weber"
  And the "Postcode" field is filled with "10115"
  And "Germany" is chosen in the "Destination country" field
  And the "Leave with a neighbour" checkbox is unchecked
  And the "Register the parcel" button is clicked
  Then the "/parcels/PX-WEB-5001" page is shown
  And the page shows "Parcel PX-WEB-5001 registered"
  And a selection of rows is retrieved from the parcels.parcels table where:
    | reference | PX-WEB-5001 |
  And the selection has 1 row
  And the parcel-events kafka event named registered key is PX-WEB-5001
```

Add the pack to the project with `axx pack add web-core` ([Choose packs](/guides/use-packs/)). This guide sets it up; the others go further:

- [Work with pages](/guides/web-pages/): forms, clicks and gestures, keys, dialogs, browser tabs, frames, downloads, the browser's clock, its connection and devices.
- [Check what pages show](/guides/web-checks/): text, elements and their state, tables, counts, files, script errors and requests.
- [Sign in to web apps](/guides/web-sign-in/): the sign-in flow, or a session to start with, and secrets.
- [Compare screenshots](/guides/web-screenshots/): pages that look as designed, each platform with its own screenshots (the `web-screenshots` pack).
- [Check accessibility](/guides/web-accessibility/): WCAG audits with axe-core, and what a screen reader reads (the `web-a11y` pack).
- [Control what pages fetch](/guides/web-network/): requests that fail, answer with a file or take their time, a slow connection, recordings, and websocket messages (the `web-network` pack).
- [Audit pages with Lighthouse](/guides/web-lighthouse/): performance, accessibility, best practices and SEO scores, and the Core Web Vitals (the `web-lighthouse` pack).
- [Measure JavaScript coverage](/guides/web-coverage/): how much of the web apps' JavaScript the scenarios run, as lcov and Istanbul reports (the `web-coverage` pack).
- [Watch and debug browsers](/guides/watch-web-browsers/): see the browsers as they go, pause a scenario in Playwright's Inspector, pick elements and record steps, traces and videos.
- [How the web-core pack works](/explanations/web-browsers/): the browsers, isolation, waiting, names and selectors.

## The browsers

The browsers run on the machine that runs Axx, as in any Playwright project: Chromium, Firefox and WebKit, the builds of Playwright 1.62.1, the version the pack drives. The pack downloads each one the first time a scenario uses it, and keeps it where Playwright keeps its browsers, for the next runs and for other Playwright projects of that version: `~/Library/Caches/ms-playwright` on macOS, `~/.cache/ms-playwright` on Linux, `%LOCALAPPDATA%\ms-playwright` on Windows (`PLAYWRIGHT_BROWSERS_PATH` moves it). The engines `chrome` and `msedge` are the Google Chrome and Microsoft Edge installed on the machine. Chromium is the whole browser, with a window or without one, so that a run behaves the same whether you watch it or not.

The browsers use your web apps at the addresses you would: `localhost` and the port their service listens on, whether it runs in Compose, publishing its port, or on your machine, from your IDE or with `axx run --debug` ([Manage the services under test](/guides/manage-services/)).

### In CI

On Linux, the browsers need system libraries that a bare machine or a slim image lacks. Install them before the run, with Playwright's command for the engines you use:

```yaml title=".github/workflows/acceptance.yml"
- run: sudo npx -y playwright@1.62.1 install-deps chromium
```

Or run the job in Playwright's image, `mcr.microsoft.com/playwright:v1.62.1-noble`, which has them. Keep Playwright's folder of browsers in the CI cache to save the download. When a library is missing, the step that opens the first page says which command installs them.

## Register the web app

Register the app with its address:

```gherkin
Background:
  Given the parcels web app with the following properties:
    | url | http://localhost:8400/portal |
```

| Property | |
| --- | --- |
| `url` | where the browser finds the app: page paths are below it (required) |
| `engine` | `chromium` (the default), `firefox`, `webkit`, or `chrome` and `msedge`: the Google Chrome and Microsoft Edge installed on the machine |
| `device` | a device Playwright knows, like `iPhone 15`, `Pixel 7` or `iPad Pro 11`: its screen size, user agent and touch screen |
| `viewport` | the size of the page, like `1280x720` (the default) |
| `locale` | the browser's language, like `de-DE` (`en-US` by default, whatever the machine's) |
| `timezone` | the browser's time zone, like `Europe/Berlin` (`UTC` by default, whatever the machine's) |
| `color scheme` | `light`, `dark` or `no-preference`: what `prefers-color-scheme` says |
| `reduced motion` | `reduce` or `no-preference`: what `prefers-reduced-motion` says |
| `media` | `screen` (the default) or `print`: the pages as they print, with their print styles |
| `location` | where the browser says it is: a latitude and a longitude, like `52.5200, 13.4050` |
| `permissions` | permissions the app has, like `notifications, clipboard-read` |
| `user agent` | the user agent the browser sends |
| `tls.verify` | `false` accepts the app's certificate whatever it is, for test environments with their own |
| `cookie.<name>`, `header.<name>`, `local storage.<key>`, `session storage.<key>`, `username`, `password` | a session to start with ([Sign in to web apps](/guides/web-sign-in/#start-with-a-session)) |

To run the same features in another engine, make it a property: `| engine | ${sys:browser.engine:-chromium} |`, then `axx run -D browser.engine=webkit`. A phone is another web app of the same address:

```gherkin
Scenario: Shops register parcels from their phones too
  Given the phone web app with the following properties:
    | url    | http://localhost:8400/portal |
    | engine | webkit                       |
    | device | iPhone 15                    |
  When the "/quote" page of the phone web app is opened
  And the "Menu" button is tapped
  And the "Register a parcel" link is tapped
  Then the "/parcels/new" page is shown
```

`the "/quote" page is opened` opens a page of the app, and the steps after it act on that page. With several web apps registered, `the "/quote" page of the parcels web app is opened` names the one.

## Find things the way people do

Steps name what people see on the page:

- **Fields** by their label: `the "Weight (grams)" field`. A field without a label is found by its placeholder.
- **Drop-downs** are fields too; `"Germany" is chosen in the "Destination country" field` picks an option by its text.
- **Radio buttons** by their label: `the "Express" option is chosen`. **Checkboxes** the same way: `the "Insure this parcel" checkbox is checked`.
- **Buttons** by their name (their text, or their label), **links** by their text, **tabs** in a row of tabs on the page by their name, and **menu items** in an open menu.
- **Elements**, anything else: by their text, their label, an image's alternative text or a title: `the "Ella Brandt" element is double-clicked`.
- **Text** anywhere on the page: `the page shows "6.90 EUR"`, part of what the page shows, where case matters.

Names match exactly. When nothing on the page has the name, the step says what the page has instead:

```text
No field named "Weight" on the page; 3 fields:
  "Weight (grams)"
  "Destination country"
  "Postcode"
```

A field that people can use but a step cannot find usually has no label. Give it one: the page becomes more accessible too. Steps look inside frames as well, such as an embedded widget or a payment form: what people see on the page, the steps find, whichever frame it is in.

### Selectors, when names are not enough

Where a name is not enough, or not there, a step takes a selector instead, in the same place: a CSS selector, an XPath, or a test id, the `data-testid` attribute of your markup.

```gherkin
Then the "testid=parcel-count" element shows "25 parcels"
And the page shows 20 "testid=parcel-row" elements
And the "xpath=//tbody/tr[20]/td[1]" element shows "PX-WEB-5220"
And the "css=.quote-result" element shows "6.90 EUR"
```

`css=`, `xpath=` and `testid=` start a selector; anything else is a name. `packs.web-core.testIdAttribute` in `axx.yaml` changes the attribute test ids are in. Prefer names where they work: they read like the page, they keep working when the markup changes, and a page people can find things on is one they can use.

## Waiting

Pages change after the fact: a price arrives from a pricing service, a message goes away. Every step waits for what it needs, so there is never a reason to sleep:

- An action waits for its element to be there and ready: visible, enabled, not moving.
- A check waits for the page to show what it expects: 10 seconds, or the time `within {duration}` gives.

```gherkin
Scenario: A quote takes a moment
  When the "/quote" page is opened
  And the "Weight (grams)" field is filled with "1200"
  And "Germany" is chosen in the "Destination country" field
  And the "Postcode" field is filled with "10115"
  And the "Get a quote" button is clicked
  Then within 5s the page shows "Price: 6.90 EUR"
  And the page does not show "Calculating"
```

## Scenarios run side by side

Every scenario has a browser context of its own, and one for each web app it registers: its own cookies, storage and pages. Scenarios run in parallel like any others, and a shop signed in in one scenario is never signed in in another. Like all parallel scenarios, give each one its own data, such as a parcel reference ([Isolate test data](/guides/isolate-test-data/)).

The [`parcels` example](https://github.com/nimbusxr/axx/tree/main/examples/parcels) has a shop portal and its features: `shop-portal.feature`, `portal-parcels.feature`, `portal-planning.feature` and `portal-tracking.feature`.
