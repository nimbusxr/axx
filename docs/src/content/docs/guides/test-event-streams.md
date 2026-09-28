---
title: Test event streams
description: Open the server-sent event streams (text/event-stream) of your services and check the events they send, by their type, their id and their JSON data.
---

A server-sent event stream is a response that doesn't end: the service writes an event each time something happens, such as a parcel's depot scans for a shop's system, and ends the stream when there is nothing more to say. The `sse` pack opens your services' streams and checks the events they send.

```gherkin
Scenario: A shop's system hears that the parcel is delivered
  Given a seeds/live-delivered.yaml db seed
  And the tracking event stream with the following properties:
    | url | http://localhost:8400/api/parcels/PX-LIV-5704/events |
  And a depot-scans kafka event
  And the depot-scans kafka event key is PX-LIV-5704
  And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then within 20s the tracking event stream has an event where:
    | event type | delivered   |
    | reference  | PX-LIV-5704 |
    | status     | DELIVERED   |
```

Add the pack to the project with `axx pack add sse` ([Choose packs](/guides/use-packs/)).

## Open a stream

Register a stream under a name. It opens there and then, and belongs to the scenario, which closes it when it ends:

```gherkin
Given the tracking event stream with the following properties:
  | url                  | https://parcels.example.com/api/parcels/PX-LIV-5704/events |
  | header.Authorization | Bearer ${env:SHOP_TOKEN}                                   |
```

| Property | What it is |
| --- | --- |
| `url` | The stream's URL (required). |
| `header.<name>` | A header of the request, such as a token. |

A stream that doesn't answer `200` with `text/event-stream` fails the step, and shows what the service answered instead, such as a `404` for an unknown parcel. `${env:..}` values are masked in logs and failures.

Open the stream before what makes the service send events, as in the scenario above: a check sees the events the stream sent since it opened.

## Check its events

A check waits for an event that meets it: 10 seconds, or the time `within` gives.

```gherkin
Then within 20s the tracking event stream has an event where:
  | event type | scan             |
  | event id   | 1                |
  | reference  | PX-LIV-5703      |
  | status     | OUT_FOR_DELIVERY |
And the tracking event stream has an event containing 'OUT_FOR_DELIVERY'
```

- `has an event where:` reads each event's data as JSON: a path and the value as text, `null` for null and `undefined` for absent. Two rows read the event itself: `event type`, its `event:` field (`message` for an event without one), and `event id`, its `id:`.
- `has an event containing {string}` looks for text in any event's data.

Comments and keep-alives are not events. An event's data that spans several `data:` lines is read as one text, its lines joined with line breaks, as a browser reads it.

A failed check lists the events the stream sent. When the service ends the stream, or the connection breaks, a check still waiting fails at once, after looking at every event that came first.

## Keep scenarios apart

A stream belongs to its scenario: another scenario running in parallel never sees its events, and it closes when its scenario ends. Follow data unique to the scenario, such as a parcel of its own ([Isolate test data](/guides/isolate-test-data/)).

See the [sse pack's reference](/references/packs/sse/) for every step.
