---
title: Work with pages
description: Do on a web app's pages what people do - fill in forms, click, tap, drag and scroll, press keys, answer dialogs, follow new browser tabs, download files - and set the browser's clock, connection and device.
---

The steps below act on the current page of the scenario's web app ([Test web apps](/guides/test-web-apps/) sets it up). Each one finds its element by the name people see, or by a selector ([Find things the way people do](/guides/test-web-apps/#find-things-the-way-people-do)), waits for it to be ready, and does what a person does.

## Forms

```gherkin
Scenario: A shop sets its pickup days, and the logo on its labels
  When the "/settings?shop=willow-and-wax" page is opened
  And the "Pickup address" field is filled with "Weserstr. 40, 12045 Berlin"
  And the following options are chosen in the "Pickup days" field:
    | Monday   |
    | Thursday |
  And the "Email me when a parcel is delivered" checkbox is checked
  And the logos/willow-and-wax.png file is uploaded in the "Upload your logo" field
  And the "Save the settings" button is clicked
  Then the page shows "Settings saved"
```

- `the "…" field is filled with "…"` replaces what a field holds. Date fields take dates as `2026-10-01`.
- `"Germany" is chosen in the "Destination country" field` picks one option of a drop-down; `the following options are chosen in the "…" field:` picks several, one a row, in a drop-down that takes more than one.
- `the "Express" option is chosen` picks a radio button; `the "…" checkbox is checked` and `… is unchecked` tick and clear a checkbox, and leave it as it is when it already is.
- `the invoices/INV-2041.pdf file is uploaded in the "Customs invoice" field` picks a file of the project, as the file dialog would: for a file field with that label, or for a button with that name that opens the file dialog.

## Clicks, taps and the pointer

- `the "Get a quote" button is clicked`, `the "Your parcels" link is clicked`, `the "History" tab is clicked` (a tab in a row of tabs on the page), `the "Cancel the parcel" menu item is clicked` (in an open menu), `the "PX-4101" element is clicked` (anything else).
- `the "Ella Brandt" element is double-clicked` and `the "Nora Fischer" element is right-clicked`, which opens the page's own menu when it has one.
- `the "Menu" button is tapped`, on a touch screen: the web app's `device` must have one, like `iPhone 15`.
- `the pointer is moved over "About Express"` moves the pointer over a button, link, tab or menu item with that name, or else over that text, so that what opens on hover opens: a menu, a tip.
- `the "Pickup PX-WEB-5302" element is dragged onto the "Friday" element` drags, as with a mouse.

```gherkin
Scenario: A shop tracks a parcel from its row's menu
  Given a seeds/portal-row-menu.yaml db seed
  And the "/parcels?shop=thistle-and-thread" page is opened
  When the "Nora Fischer" element is right-clicked
  And the "Track this parcel" menu item is clicked
  Then the "/track/PX-WEB-5511" page is shown
```

## Keys

Keys go to a field, or to whatever has the focus: `the Enter key is pressed in the "Find a parcel" field`, `the Escape key is pressed`. Key names are Playwright's: `Enter`, `Escape`, `Tab`, `ArrowDown`, `Backspace`, a letter, and combinations like `Control+A`.

## Scrolling

Actions scroll to their element on their own. A page that shows more as it scrolls needs more: `the page is scrolled to the bottom` scrolls as far as the page goes, and `the "Pickups" element is scrolled into view` scrolls until it shows (in the page, or in the list it is in). `the page is scrolled to the top` scrolls back.

## Dialogs

A dialog a page opens (a message, a confirmation such as "Cancel parcel PX-WEB-5121?", or a question) waits for an answer, as it does for people, and so does the page: until the dialog is answered, the other steps fail and say that a dialog is open.

```gherkin
Scenario: The portal asks before it cancels a parcel
  Given a seeds/portal-ask.yaml db seed
  And the "/parcels/PX-WEB-5124" page is opened
  And the "Actions" button is clicked
  When the "Cancel the parcel" menu item is clicked
  Then the dialog shows "Cancel parcel PX-WEB-5124?"

Scenario: A shop cancels a parcel before it is picked up
  Given a seeds/portal-cancel.yaml db seed
  And the "/parcels/PX-WEB-5121" page is opened
  And the "Actions" button is clicked
  And the "Cancel the parcel" menu item is clicked
  When the dialog is accepted
  Then the page shows "Parcel PX-WEB-5121 cancelled"
```

`the dialog is dismissed` answers Cancel, and `the dialog is answered with "The recipient moved"` types the answer to a question and accepts it.

## Browser tabs, going back, reloading

A link that opens a new browser tab, such as a tracking page for the recipient, makes it the current tab, as a browser shows it; the steps then act on it. `the browser tab is closed` closes it, and the tab it was opened from is the current tab again.

```gherkin
Scenario: A shop shares a parcel's tracking page with its recipient
  Given a seeds/portal-share.yaml db seed
  And the "/parcels/PX-WEB-5117" page is opened
  When the "Track this parcel" link is clicked
  Then the "/track/PX-WEB-5117" page is shown
  And the page shows "Your parcel from maple-crafts is in transit."

Scenario: The tracking page opens in a browser tab of its own
  Given a seeds/portal-share-closed.yaml db seed
  And the "/parcels/PX-WEB-5118" page is opened
  And the "Track this parcel" link is clicked
  When the browser tab is closed
  Then the "/parcels/PX-WEB-5118" page is shown
```

`the browser's back button is clicked` goes back a page, and `the page is reloaded` reloads it.

## Frames

Steps look inside frames: an embedded map, a pickup point chooser, a payment form from another address. Nothing to do: what people see on the page, the steps find, whichever frame it is in.

## Downloads

A file the page downloads, by a link or a button, is checked by the name the browser gives it:

```gherkin
When the "Download the label" menu item is clicked
Then the "PX-WEB-5111-label.zpl" file is downloaded
And the downloaded "PX-WEB-5111-label.zpl" file is identical to the labels/PX-WEB-5111.zpl file
```

`the downloaded "parcels.csv" file contains "PX-WEB-5101,Lena Hoffmann"` checks part of it ([Check what pages show](/guides/web-checks/#downloaded-files)). A failed scenario keeps the downloads its steps checked in `.axx/web/downloads/`. WebKit opens some files in the page instead of downloading them, text files among them, unless their link has the `download` attribute, as a download link should.

## The browser's clock

Pages that show the time, count down, or end a session after a while need a clock the scenario sets:

```gherkin
Scenario: The portal ends a session after 30 minutes without activity
  Given the browser's clock is set to "2026-09-25T09:00:00+02:00"
  And the "/quote" page is opened
  When the browser's clock is moved forward by 31m
  Then the page shows "Your session ended after 30 minutes without activity"
```

`the browser's clock is set to "…"` sets the time the pages see, a time like `2026-09-25T14:00:00+02:00` (or `2026-09-25 14:00`, in UTC). Time goes on from there, so the pages' timers run. `the browser's clock is moved forward by 31m` jumps ahead, as if the computer slept: the timers due in between fire once, and what expires expires. Set the clock before opening the pages whose timers it moves; it holds for every web app of the scenario.

## The connection

`the browser is offline` cuts the web apps' pages off from everything, and `the browser is online` brings them back: for pages that say they are offline, keep a draft, or sync later.

```gherkin
Scenario: The portal says when it cannot save anything
  Given the "/quote" page is opened
  When the browser is offline
  Then the page shows "You are offline: nothing is saved until you are back online"

Scenario: The portal says when it can save again
  Given the "/quote" page is opened
  And the browser is offline
  When the browser is online
  Then the page does not show "You are offline"
```

## Devices, languages and more

How the browser presents itself is up to the web app's properties ([Register the web app](/guides/test-web-apps/#register-the-web-app)): a `device` (a phone, a tablet), a `viewport`, a `locale` and a `timezone`, a `color scheme` (dark mode), `reduced motion`, the `media` (a page as it prints), a `location`, `permissions` and a `user agent`. A web app that is the same address with other properties is another browser for the same app: a phone next to a desktop, a shop in Berlin next to one in New York.

```gherkin
Scenario: A shop in Germany sees times in its own time zone
  Given the berlin web app with the following properties:
    | url      | http://localhost:8400/portal |
    | locale   | de-DE                        |
    | timezone | Europe/Berlin                |
  And a seeds/portal-times.yaml db seed
  When the "/parcels/PX-WEB-5131" page of the berlin web app is opened
  Then the page shows "Registered: 25.09.2026, 14:03"
```

Pages learn the browser's `location` on secure addresses only (`https://`, or `localhost`), as browsers allow.
