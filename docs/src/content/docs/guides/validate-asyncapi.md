---
title: Validate against AsyncAPI
description: Check every message a scenario sends and every message a check finds against your AsyncAPI document, over Kafka, AMQP, MQTT, NATS, cloud queues and topics, WebSockets and event streams, and relax individual rules for negative tests.
---

Give a broker's registration an `asyncapi` property and Axx checks messages against that document:

- **Every message a scenario sends,** before it goes. This catches test data that drifted from the contract.
- **Every message a check finds,** before the check passes. This catches a service that broke the contract.

A message that breaks the document fails the step and names the rule it broke. Your AsyncAPI document stays the contract for your events, as your OpenAPI document is for your requests ([Validate against OpenAPI](/guides/validate-openapi/)).

```gherkin
Scenario: A scanner is alerted when it scans a parcel the service does not know
  Given the depots mqtt broker with the following properties:
    | url      | mqtt://localhost:1883                 |
    | asyncapi | http://localhost:8400/asyncapi.yaml   |
  When the mqtt/scan-unknown.json message is published to the depots/LEJ/scans mqtt topic
  Then within 10s the depots/+/alerts mqtt topic has a message where:
    | parcelRef | PX-MQT-8299    |
    | problem   | unknown parcel |
```

Add the pack to the project with `axx pack add asyncapi` ([Choose packs](/guides/use-packs/)). An `asyncapi` row without it fails the registration and says so.

## Turn it on

`asyncapi` is a URL or a file path (resolved against `resources`), in YAML or JSON. AsyncAPI 3.x and 2.x (2.6 and earlier) are supported, with `$ref`s to other files and URLs, relative to the document that holds them.

Point it at the document your service serves, or at the file in your repository. The row works on every registration that messages go through:

| Registration | Its messages |
| --- | --- |
| `the {word} kafka service` | the events of its topics |
| `the {word} amqp broker` | its queues and exchanges |
| `the {word} mqtt broker` | its topics |
| `the {word} nats server` | its subjects and streams |
| `the {word} aws account` | its SQS queues and SNS topics |
| `the {word} gcp project` | its Pub/Sub topics |
| `the {word} service bus namespace` | its queues and topics |
| `the {word} websocket` | the messages both ways |
| `the {word} event stream` | its events |

## How a message finds its place in the document

A message is checked against a **channel**, then one of the channel's **messages**:

- **Channels, by address.** The channel whose address is where the message goes: a topic, a queue, a subject, or an exchange then its routing key. For a WebSocket or an event stream, it is the path of the URL; a server's `pathname` may come before it.
  - A `{parameter}` stands for any text without a slash: `depots/{depot}/scans` is the channel of `depots/LEJ/scans`, and `tracking.{reference}` of `tracking.PX-NAT-8301`.
  - A check on a filter or a stream (`the depots/+/alerts mqtt topic`, `the TRACKING nats stream`) looks for the channel of the topic or subject each message came on.
  - When channels name their servers, only the servers of the registration's protocol count, so one document can describe the same address on Kafka and on MQTT.
- **Messages, by their schemas.** The message's payload and headers must follow the schemas of one of the channel's messages. An event stream's event type picks the message of that name (`scan`, `delivered`).
  - When none fits, the failure names the message it comes closest to: the one that declares the most of its properties.
- **Schemas.**
  - JSON Schema is draft 7 unless the schema says otherwise, and AsyncAPI's own schema format is read as JSON Schema.
  - Avro payloads (`schemaFormat: application/vnd.apache.avro;version=1.9.0`) are in Avro's JSON encoding, as the Kafka pack publishes them.
  - Payloads are JSON unless their content type says YAML or text.
  - A message's `contentType` (or the document's `defaultContentType`) is checked against the content type the message has, when it has one.

## Validation levels

Each rule has a key and a level:

| Key | Found when |
| --- | --- |
| `validation.channel.unknown` | the document has no channel for the message |
| `validation.message.contentType` | the message's content type is not its message's |
| `validation.message.payload.format` | the payload is not JSON (or YAML) |
| `validation.message.payload.schema.<keyword>` | the payload breaks a JSON Schema keyword (`required`, `type`, `enum`...), or `avro` its Avro schema |
| `validation.message.headers.schema.<keyword>` | the headers break a keyword of their schema |

`ERROR` fails the step, `WARN` and `INFO` log the finding, and `IGNORE` drops it. Everything is `ERROR` by default; `FAIL` is an alias of `ERROR`.

A key also sets the keys below it, and the most specific key set wins: `validation.message.payload` relaxes `validation.message.payload.schema.required` too. A key must be one Axx reports, or a prefix of such keys: a misspelled key fails the step (or the run, in `axx.yaml`), and the message names the closest keys.

### Relax a rule in a scenario

A scenario that sends a broken message on purpose, to check what the service does with it, relaxes only what it breaks:

```gherkin
Scenario: A report without a parcel reference is set aside
  Given the AsyncAPI validation levels are:
    | validation.message.payload | IGNORE |
  When a message is sent to the parcels.label-printed amqp queue:
    """
    {"printer": "LEJ-3", "job": "JOB-AMQ-8105"}
    """
  Then within 10s the parcels.label-printed.rejected amqp queue has a message where:
    | job           | JOB-AMQ-8105       |
    | header reason | not a label report |
```

`WARN` keeps a known gap visible in the logs without failing: the example's older printers name themselves in a header, not in the body the contract describes.

```gherkin
Scenario: An older printer reports straight to the service's queue
  Given a seeds/printing-queue.yaml db seed
  And the AsyncAPI validation levels are:
    | validation.message.payload.schema.required | WARN |
  When the amqp/label-printed-PX-AMQ-8103.json message is sent to the parcels.label-printed amqp queue with the following properties:
    | header printer | DRS-1 |
```

The levels apply to every contract the scenario checks messages against, until it ends.

### Defaults for the whole suite

`packs.asyncapi.levels` in `axx.yaml` sets the defaults the scenarios' levels are merged over. Use it for a known gap in a document you do not control:

```yaml title="axx.yaml"
packs:
  asyncapi:
    levels:
      validation.message.contentType: WARN
```

## Build messages from the document

A fixture factory builds messages from the payload of an AsyncAPI message, the way it builds request payloads from OpenAPI components ([Fixture factories](/guides/fixture-factories/)):

```yaml title="mqtt/scans.factory.yaml"
factory:
  family: json
  schema: ../../app/asyncapi.yaml#/components/messages/scan
prototype:
  location: Leipzig
identity:
  - path: scanId
    derive: authored
fixtures:
  scan-unknown:
    scanId: SC-8299-1
    parcelRef: PX-MQT-8299
    status: IN_TRANSIT
```

The payload's `$ref`s into the document's components resolve, and so do those to other files. An Avro payload is built with the `avro` family, from its `.avsc` file.

See the [asyncapi pack's reference](/references/packs/asyncapi/) for the step and the keys.
