---
title: Control what pages fetch
description: Make a page's requests fail, answer with a status or a file, or take their time, slow the browser's connection, answer from a recording, and check websocket messages.
---

Pages fetch from the browser: an estimate, a price, a map, live updates over a websocket. The `web-network` pack sees and changes those requests, to check what a page does when an answer is late, wrong or missing. It builds on the `web-core` pack, which comes with it: `axx pack add web-network` adds both.

These are the pages' own requests. The services your app calls from its server are mocked on the server's side instead ([Mock dependencies](/guides/mock-dependencies/)).

## Name a request

A step names requests by their address:

- a path below the web app's `url`: with the `url` `http://localhost:8400/portal`, `"/pickups"` is `http://localhost:8400/portal/pickups`;
- or a whole URL, for another site: `"https://maps.example.com/tiles/**"`.

`*` stands for any part of a path (`"/track/*/estimate"`), and `**` for any parts. The query doesn't count, unless the address has one: `"/pickups"` names `/pickups?day=Friday` too, and `"/pickups?day=Friday"` only that one.

What a step sets holds for every web app of the scenario, from their next request on. Set it before the page opens, so that the page's first requests get it too. When two steps name the same request, the later one answers it.

## Requests that fail or answer

```gherkin
Scenario: A shop is told when a pickup cannot be planned for want of a connection
  Given a seeds/portal-pickup-offline.yaml db seed
  And the page's requests to "/pickups" fail
  And the "/parcels?shop=fern-and-flax" page is opened
  When the "Pickup PX-WEB-5321" element is dragged onto the "Tomorrow" element
  Then the page shows "The pickup could not be planned: check your connection and try again"
```

- `the page's requests to "/pickups" fail`: the request fails as when the service is down or out of reach. The page's `fetch` throws.
- `the page's requests to "/pickups" answer with status 503`: the request gets that status and nothing else.
- `the page's requests to "/track/*/estimate" answer with the network/estimate-tuesday.json file`: the request gets a file of the project, with the type of its extension. This is for an answer the page only shows, such as one that depends on the day.
- `the page's requests to "/track/*/estimate" take 5s`: the request goes out that much later, as to a busy service. The page shows what it shows while it waits.

```gherkin
Scenario: The tracking page says it is still working out when the parcel arrives
  Given a seeds/portal-tracking-waiting.yaml db seed
  And the page's requests to "/track/*/estimate" take 5s
  When the "/track/PX-WEB-5602" page is opened
  Then the page shows "Working out when your parcel arrives"
```

To cut the pages off from everything, use `the browser is offline` ([The connection](/guides/web-pages/#the-connection)).

## A slow connection

`the browser's connection is slow` makes the browser's connection that of a phone with a poor signal: 400 ms more for every request, and 500 kbit/s each way. It works in Chromium, Chrome and Edge only: with another engine, the step fails, or the one that opens the page.

```gherkin
Scenario: The tracking page works on a phone with a poor signal
  Given a seeds/portal-tracking-slow.yaml db seed
  And the browser's connection is slow
  When the "/track/PX-WEB-5604" page is opened
  Then the page shows "Arrives on Monday, September 28, between 09:00 and 18:00"
```

The checks wait for the page as usual, 10 seconds or `within {duration}`.

## Answer from a recording

With `record: true`, each scenario's pages record what they fetch, in a HAR file for each web app in `.axx/web/recordings`:

```sh
axx run --set packs.web-network.record=true features/portal-tracking.feature:52
```

Copy a recording into the project and keep the requests you want answered. `the page's requests are answered from the network/tracking-delivered.har recording` then answers the requests the recording has, as they were recorded. The requests it lacks go out, and so do websockets.

```gherkin
Scenario: A delivered parcel's tracking page says when it was delivered
  # Recorded from the service, with the parcel's delivery scan in the tracking store.
  Given a seeds/portal-tracking-delivered.yaml db seed
  And the page's requests are answered from the network/tracking-delivered.har recording
  When the "/track/PX-WEB-5605" page is opened
  Then the page shows "Delivered on Thursday, September 24, at 10:12"
```

A request matches a recorded one by its URL and method, and its body for a `POST`. So record with the web app's `url` the scenarios use.

## Websocket messages

The pack listens to the websockets of the scenario's pages from the start:

- `the page sent a websocket message containing "PX-WEB-5607"` checks what a page sent;
- `the page received a websocket message containing "OUT_FOR_DELIVERY"` checks what it received.

Both wait for the message, 10 seconds or `within {duration}`. When it doesn't come, the step lists the messages that did.

```gherkin
Scenario: The tracking page shows a depot scan as it happens
  Given a seeds/portal-tracking-live.yaml db seed
  And the "/track/PX-WEB-5606" page is opened
  And a depot-scans kafka event
  And the depot-scans kafka event key is PX-WEB-5606
  And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
  And the depot-scans kafka event payload properties are:
    | $.scanId    | SC-5606-1   |
    | $.parcelRef | PX-WEB-5606 |
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then the page received a websocket message containing "OUT_FOR_DELIVERY"
  And the page shows "Out for delivery from Leipzig"
```

## Settings

| Setting | |
| --- | --- |
| `record` | record what each scenario's pages fetch, a HAR file for each web app in `.axx/web/recordings` (default `false`) |

In Chromium, a page that a step answers (from a file or a recording) is not one the browser got from the network. So the browser would treat it as a page of the internet, and keep it from reaching the machine's own addresses, where the app's websockets and services are. The pack lets such pages reach them.
