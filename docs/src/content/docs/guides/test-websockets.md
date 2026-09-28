---
title: Test WebSockets
description: Open WebSocket connections to your services, send messages on them, and check the messages they receive and the code they close with.
---

A WebSocket keeps a connection open both ways: a page follows a parcel's scans as they happen, a courier's app gets its next stop. The `websocket` pack opens connections to your services, sends messages on them and checks what comes back, whatever made the service send it: a Kafka event, a row, a request.

```gherkin
Scenario: The tracking page hears that its parcel is out for delivery
  Given a seeds/live-websocket.yaml db seed
  And the tracking websocket with the following properties:
    | url | ws://localhost:8400/portal/track/PX-LIV-5701/live |
  And a message is sent to the tracking websocket:
    """
    {"follow": "PX-LIV-5701"}
    """
  And a depot-scans kafka event
  And the depot-scans kafka event key is PX-LIV-5701
  And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then within 20s the tracking websocket received a message where:
    | reference | PX-LIV-5701      |
    | status    | OUT_FOR_DELIVERY |
```

Add the pack to the project with `axx pack add websocket` ([Choose packs](/guides/use-packs/)). To check what a page in a browser sends and receives on its WebSockets, use the web packs instead ([Web network](/guides/web-network/)).

## Open a connection

Register a connection under a name. It opens there and then, and belongs to the scenario, which closes it when it ends:

```gherkin
Given the tracking websocket with the following properties:
  | url                  | wss://parcels.example.com/portal/track/PX-LIV-5701/live |
  | header.Authorization | Bearer ${env:SHOP_TOKEN}                                |
  | subprotocol          | parcels.v1                                              |
```

| Property | What it is |
| --- | --- |
| `url` | The `ws://` or `wss://` URL (required). |
| `header.<name>` | A header of the opening request, such as a token. |
| `subprotocol` | The subprotocol to ask for. A server that picks none is still accepted. |
| `asyncapi` | The AsyncAPI document the messages both ways follow ([Validate against AsyncAPI](/guides/validate-asyncapi/)). |

A connection the server refuses fails the step, with what the server answered. `${env:..}` values, such as a token, are masked in logs and failures.

## Send messages

Send a text message from the doc string, or from a file of the project:

```gherkin
When a message is sent to the tracking websocket:
  """
  {"follow": "PX-LIV-5701"}
  """
And the tracking/follow.json message is sent to the tracking websocket
```

## Check what it received

A check looks at every message the connection received since it opened, and waits for one that meets it: 10 seconds, or the time `within` gives.

```gherkin
Then within 20s the tracking websocket received a message where:
  | reference | PX-LIV-5701      |
  | status    | OUT_FOR_DELIVERY |
  | location  | Leipzig          |
And the tracking websocket received a message containing 'PX-LIV-5701'
```

- `received a message where:` reads each message as JSON: a path and the value as text, `null` for null and `undefined` for absent, like the message checks of queues and topics.
- `received a message containing {string}` looks for text in any message.

A failed check lists the messages the connection received. A binary message shows as its size.

## Check how it closed

When a server closes a connection, a check still waiting for a message fails at once, and says why. Check the code the server closed it with:

```gherkin
Scenario: The tracking websocket closes a connection that does not say which parcel it follows
  Given the tracking websocket with the following properties:
    | url | ws://localhost:8400/portal/track/PX-LIV-5702/live |
  When a message is sent to the tracking websocket:
    """
    {"parcel": "PX-LIV-5702"}
    """
  Then the tracking websocket was closed with code 1008
```

The check waits for the connection to close, like the others.

## Keep scenarios apart

A connection belongs to its scenario: another scenario running in parallel never sees it or its messages, and it closes when its scenario ends. What a service sends on a connection often depends on shared data, so follow data unique to the scenario, such as a parcel of its own ([Isolate test data](/guides/isolate-test-data/)).

See the [websocket pack's reference](/references/packs/websocket/) for every step.
