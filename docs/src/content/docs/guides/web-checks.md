---
title: Check what pages show
description: Check a web app's pages - the text they show, their elements and their state, tables and counts, the page and its title, downloaded files, script errors and the requests the page sent.
---

Every check waits for what it expects: 10 seconds, or the time `within {duration}` gives ([Waiting](/guides/test-web-apps/#waiting)). When it does not come, the check says what the page shows instead.

## Text

`the page shows "Price: 6.90 EUR"` looks for the text anywhere on the page, in every frame, as part of what the page shows; case matters. `the page does not show "Calculating"` waits for it to go. When the text is not there, the failure shows the lines of the page most like it:

```text
The page does not show "Price: 99.00 EUR"
  expected: "Price: 99.00 EUR"
  actual:   "Price: 6.90 EUR · delivered in 2 days"
```

## Elements

Elements are named as people see them, or by a selector ([Find things the way people do](/guides/test-web-apps/#find-things-the-way-people-do)):

```gherkin
Scenario: A parcel on its way can no longer be cancelled
  Given a seeds/portal-track.yaml db seed
  And the "/parcels/PX-WEB-5112" page is opened
  When the "Actions" button is clicked
  Then the "Download the label" menu item is shown
  And the "Cancel the parcel" menu item is not shown
  And the "Actions" button has the aria-expanded attribute "true"
  And the "Track this parcel" link has the href attribute "/portal/track/PX-WEB-5112"
```

- `the "…" button is shown` and `… is not shown`, for any kind of element: a `button`, `field`, `checkbox`, `option`, `link`, `tab`, `menu item` or `element`.
- `the "testid=quote" element shows "6.90 EUR"`: the text of one element.
- `the "Actions" button has the aria-expanded attribute "true"`: one of its attributes.
- `the "Weight (grams)" field has the focus`: where the keyboard types.
- `the "Weight (grams)" field has the value "1200"`: what a field holds, or for a drop-down, the text of the option chosen.

## State

- `the "Leave with a neighbour" checkbox is ticked` and `… is not ticked`.
- `the "Standard" option is selected` and `… is not selected`, for radio buttons.
- `the "Register the parcel" button is disabled` and `… is enabled`; the same for fields: `the "Shop account" field is disabled`.

## Counts and tables

```gherkin
Scenario: A shop sees its parcels
  Given a seeds/portal-busy.yaml db seed
  When the "/parcels?shop=harbour-hardware" page is opened
  Then the page shows 20 "testid=parcel-row" elements
  And the page shows a table row where:
    | Parcel | PX-WEB-5205 |
    | Status | IN_TRANSIT  |
  And the page shows 3 table rows where:
    | Status | IN_TRANSIT |
```

- `the page shows 3 "css=.parcel-card" elements` counts what the page shows, by name or selector.
- `the page shows a table row where:` finds a row by the table's column headers, and `the page shows 3 table rows where:` counts them. A table's headers are its `<thead>`, or a first row of header cells.

## The page

- `the "/parcels/PX-WEB-5001" page is shown` is where the browser is: the path below the app's `url`, with the query only when the step gives one, or a whole URL.
- `the page title is "Get a quote · Parcels"` is the title the browser tab shows.

## Downloaded files

```gherkin
When the "Export as CSV" link is clicked
Then the "parcels-birch-and-bloom.csv" file is downloaded
And the downloaded "parcels-birch-and-bloom.csv" file contains "PX-WEB-5101,Lena Hoffmann,EXPRESS,IN_TRANSIT"
And the downloaded "parcels-birch-and-bloom.csv" file has a row where:
  | Parcel | PX-WEB-5101 |
  | Status | IN_TRANSIT  |
```

`the "…" file is downloaded` waits for the browser to download a file with that name, and `the downloaded "…" file is identical to the labels/PX-WEB-5111.zpl file` compares it with a file of the project. `the downloaded "…" file contains "…"` checks part of its text, as its type has it: the text of a PDF's pages, the paragraphs and tables of a Word document (`.docx`), the cells of an Excel workbook (`.xlsx`), or the text itself. `the downloaded "…" file has a row where:` finds a row of a CSV or TSV file, or of an Excel workbook's first sheet, by the columns its first row names.

## Script errors

`the page has no script errors` checks that the web app's pages had no errors in their scripts since the scenario opened them: none thrown and not caught, and none logged to the console. A failed scenario lists its pages' script errors either way. With `packs.web-core.scriptErrors: fail` in `axx.yaml`, any script error fails its scenario, without the step.

The browser logs to the console what it fails to load too, like the site's icon: a browser asks for `/favicon.ico` unless the page names its icon (`<link rel="icon" href="...">`), and a site without one has `Failed to load resource: the server responded with a status of 404 (Not Found)` in every page's console. Give the app its icon.

## Requests the page sent

`the browser sent a POST request to "/quote"` checks that the page sent a request with that method to that address: a path below the web app's `url` (the query counts only when the step gives one), or a whole URL. What a service did with a request is best checked on the service (its database, its events, its mocks); this step is for what only the browser knows about, such as a call to a third party.
