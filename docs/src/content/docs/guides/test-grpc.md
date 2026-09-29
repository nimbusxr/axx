---
title: Call gRPC services
description: Call your gRPC services, unary and server-streaming, with requests from tables or JSON files, and check their status, answers, metadata and streamed messages, read with your protos or the server's reflection.
---

The `grpc` pack calls your services over gRPC and checks what they answer: a unary call's status and answer, and the messages of a server stream as they come.

```gherkin
Scenario: A shop's system reads a parcel's status
  Given a seeds/grpc-parcel.yaml db seed
  And the tracking grpc service with the following properties:
    | address | localhost:8410 |
  When the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the following fields:
    | reference | PX-GRP-6101 |
  Then the tracking grpc service answered OK
  And the tracking grpc service's answer has the following fields:
    | status       | REGISTERED |
    | serviceLevel | EXPRESS    |
    | lastScan     | undefined  |
```

Add the pack to the project with `axx pack add grpc` ([Choose packs](/guides/use-packs/)). To mock the gRPC services *your* service calls, see [Mock gRPC dependencies](/guides/mock-dependencies/#mock-grpc-dependencies).

## Register the service

```gherkin
Background:
  Given the tracking grpc service with the following properties:
    | address              | ${sys:local.host}:8410     |
    | proto                | protos/tracking.proto      |
    | header.authorization | Bearer ${env:SHOP_TOKEN}   |
```

| Property | What it is |
| --- | --- |
| `address` | The service's `host:port` (required). |
| `proto` | Its `.proto` file, compiled by Axx with its imports, or a descriptor set (`protoc --descriptor_set_out --include_imports`). |
| `tls` | `true` for a TLS connection, checked against the system's certificates. Default `false`. |
| `header.<name>` | Metadata sent with every call, such as a token. |
| `timeout` | The deadline of each unary call. Default `10s`. |

**The proto is the contract.** Without a `proto`, Axx reads the service's descriptors through its [server reflection](https://grpc.io/docs/guides/reflection/), once per run. Requests are built with them, so a field the request message does not have fails the step before the call is made. Answers are read with them.

`${env:..}` values are masked in logs and failures, and `${token:..}` names a token the scenario registered ([Test webhooks and tokens](/guides/webhooks-and-tokens/)).

## Call a method

A method is `package.Service/Method`, as [grpcurl](https://github.com/fullstorydev/grpcurl) names it. `Service/Method` or `Method` do when only one of the service's methods has that name.

```gherkin
When the Tracking/GetParcel method is called on the tracking grpc service with the following fields:
  | reference | PX-GRP-6101 |
When the Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message
When the Tracking/GetParcel method is called on the tracking grpc service with the grpc/get-parcel.json message and the following fields:
  | reference | PX-GRP-6102 |
When the Tracking/ListDepots method is called on the tracking grpc service
```

- **The request is proto JSON:** fields by their JSON names (`lastScan.location`) or their proto names (`last_scan.location`), enums by name, 64-bit integers as numbers or text.
- **A table sets paths into the request,** as the REST pack's request properties do. A value takes the type of its field, so `10115` is text for a string field. Double quotes make a value text, `null` leaves the field unset, and `undefined` removes it.
- **A status other than OK** does not fail the step that makes the call: check it. Only a call that cannot be made (an unknown method, a request that is not the method's message) fails the step.

## Check the answer

```gherkin
Then the tracking grpc service answered OK
Then the tracking grpc service answered NOT_FOUND with a message containing 'PX-GRP-6199'
And the tracking grpc service's answer has the following fields:
  | reference         | PX-GRP-6101 |
  | lastScan.location | Leipzig     |
And the tracking grpc service's answer has the following metadata:
  | x-served-by | tracking-v1 |
```

- **Statuses** are gRPC's names: `OK`, `CANCELLED`, `INVALID_ARGUMENT`, `NOT_FOUND`, `FAILED_PRECONDITION`, `UNAVAILABLE`, and so on, or their numbers.
- **The answer is proto JSON,** with the fields that have their default values (`0`, `""`, `false`): an unset message field, such as a parcel's `lastScan` before its first scan, is `undefined`.
- **Metadata** is the call's headers and trailers, by name in any case; several values are joined with `, `.
- **The checks read the service's last call** in the scenario.

## Watch a server stream

A server-streaming method starts when it is called, and its messages come in as the scenario goes on. Start it before what makes the service send them:

```gherkin
Scenario: A shop's system watches its parcel go out for delivery
  Given a seeds/grpc-watch.yaml db seed
  And the Tracking/WatchParcel method is called on the tracking grpc service with the following fields:
    | reference | PX-GRP-6102 |
  And a depot-scans kafka event
  And the depot-scans kafka event key is PX-GRP-6102
  And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then within 20s the tracking grpc service streamed a message where:
    | reference | PX-GRP-6102      |
    | status    | OUT_FOR_DELIVERY |
```

- `streamed a message where:` waits for a message with those values: 10 seconds, or `within {duration}`. When the stream has ended, it fails at once, with the status it ended with.
- `answered OK` waits for the stream to end, and checks the status it ended with.
- **The stream belongs to the scenario,** which closes it when it ends.

Client-streaming and bidirectional methods are not supported.

## Keep scenarios apart

Scenarios run in parallel against the same service, so each calls with data of its own, such as its parcel's reference, and its streams see only what their calls asked for.

See the [grpc pack's reference](/references/packs/grpc/) for every step.
