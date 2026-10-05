---
title: Test message brokers
description: Send and publish messages to AMQP queues and exchanges (RabbitMQ, ActiveMQ Artemis), MQTT topics and NATS subjects, and check the messages your services send there, JetStream streams included.
---

Services talk through brokers: label printers report over AMQP, a depot's handheld scanners publish over MQTT, and couriers' confirmations and tracking updates fan out over NATS. The `amqp`, `mqtt` and `nats` packs let a scenario do what the other side does, such as publishing the printer's report. The scenario then checks what your service did: the row it wrote, or the message it sent on.

```gherkin
Scenario: A report of a parcel the service does not know is set aside
  When the amqp/label-printed-unknown.json message is published to the printers amqp exchange with the following properties:
    | routing key    | printed.LEJ |
    | header printer | LEJ-3       |
  Then within 10s the parcels.label-printed.rejected amqp queue has a message where:
    | reference      | PX-AMQ-8199    |
    | header reason  | unknown parcel |
    | header printer | LEJ-3          |
```

## The packs

| Pack | Talks to | A step names |
| --- | --- | --- |
| `amqp` | RabbitMQ over AMQP 0-9-1, and brokers that speak AMQP 1.0, such as ActiveMQ Artemis | an `amqp queue` or an `amqp exchange` |
| `mqtt` | MQTT 5 brokers: Mosquitto, EMQX, HiveMQ, RabbitMQ's MQTT plugin | an `mqtt topic` |
| `nats` | NATS, with JetStream | a `nats subject` or a `nats stream` |

The three speak the same grammar as the cloud packs' queues and topics ([Test cloud services](/guides/test-cloud-services/)). Add the ones you use with `axx pack add amqp mqtt nats` ([Choose packs](/guides/use-packs/)).

## Connect to the broker

Register the broker once, usually in the `Background`. Every step of that pack in the scenario uses it:

```gherkin
Background:
  Given the depot amqp broker with the following properties:
    | url | amqp://parcels:${env:RABBITMQ_PASSWORD}@${sys:local.host}:5672/ |
  And the depots mqtt broker with the following properties:
    | url      | mqtt://${sys:local.host}:1883 |
    | username | depot-scanners                |
    | password | ${env:MQTT_PASSWORD}          |
  And the tracking nats server with the following properties:
    | url   | nats://${sys:local.host}:4222 |
    | token | ${env:NATS_TOKEN}             |
```

- **AMQP:** a `url` with the user, password and virtual host, and a `protocol`: `0-9-1` (the default) or `1.0`.
- **MQTT:** a `url` (`mqtt://`, `mqtts://`, or `ws://` and `wss://` for MQTT over WebSockets), and a `username` and `password`.
- **NATS:** a `url`, or several separated by commas, and one way to sign in: a `token`, a `username` and `password`, or a `creds` file.

`${env:..}` values, such as passwords, are masked in logs and failures, and so is a password written in a URL.

An `asyncapi` row names the AsyncAPI document the broker's messages follow: every message a scenario sends there, and every message a check finds, is checked against it ([Validate against AsyncAPI](/guides/validate-asyncapi/)).

With AMQP 1.0, a queue is an anycast address and an exchange a multicast one, as ActiveMQ Artemis and Qpid have them, and the routing key is the message's subject. RabbitMQ routes the same messages over 0-9-1 whatever protocol your services use, so keep the default for it.

## Send and publish

A message's body is the doc string, or a file of the project:

```gherkin
When a message is published to the printers amqp exchange with the routing key 'printed.LEJ':
  """
  {"reference": "PX-AMQ-8102", "printer": "LEJ-3", "printedAt": "2026-09-28T08:15:00Z"}
  """
When a message is sent to the parcels.label-printed amqp queue:
  """
  {"printer": "LEJ-3", "job": "JOB-AMQ-8105"}
  """
When a message is published to the depots/LEJ/scans mqtt topic:
  """
  {"scanId": "SC-8201-1", "parcelRef": "PX-MQT-8201", "status": "OUT_FOR_DELIVERY"}
  """
When the nats/PX-NAT-8303-delivered.json message is published to the deliveries.confirmed nats subject with the following headers:
  | courier | CR-LEJ-12 |
```

A file's message takes a table of what goes with it:

| Pack | Rows |
| --- | --- |
| `amqp` | `routing key` (exchanges), `header <name>`, `content type`, `correlation id`, `message id`, `reply to` |
| `mqtt` | `qos` (`1` unless it says otherwise), `retain` (`false`), `property <name>` (user properties), `content type`, `response topic`, `correlation data` |
| `nats` | a header on each row: its name and its value |

A few more things happen when a message is sent:

- **AMQP messages are persistent,** and a message whose body is JSON is sent as `application/json` unless a `content type` row says otherwise.
- **A message that goes nowhere fails the step:** the broker returns an AMQP message that no queue is bound for, and an MQTT broker may say nothing subscribes to a topic. The step says so, rather than letting the message vanish.
- **A NATS message waits for its stream:** when a JetStream stream captures the subject, the message is published through JetStream, and the step waits for the stream to store it.

## Check what your services sent

A check waits for a message that meets it: 10 seconds, or the time `within` gives. It looks only at the messages received since its scenario started.

```gherkin
Then within 10s the labels amqp exchange has a message where:
  | routing key   | print.express |
  | reference     | PX-AMQ-8101   |
  | header sender | alder-and-ash |
Then within 10s the depots/+/alerts mqtt topic has a message where:
  | topic            | depots/LEJ/alerts |
  | parcelRef        | PX-MQT-8299       |
  | property scanner | LEJ-HANDHELD-7    |
Then within 20s the TRACKING nats stream has a message where:
  | subject        | tracking.PX-NAT-8303 |
  | header courier | CR-LEJ-12            |
```

Each row is a path into the message's JSON body and the value it has, as text: `null` for null and `undefined` for absent. A few rows are on the message itself instead:

| Pack | Rows on the message |
| --- | --- |
| `amqp` | `header <name>`, `routing key`, `content type`, `correlation id`, `message id`, `reply to` |
| `mqtt` | `property <name>`, `topic`, `content type`, `response topic`, `correlation data` |
| `nats` | `header <name>`, `subject` |

A check can name many topics or subjects at once: MQTT's `+` and `#` (`the depots/+/alerts mqtt topic`) and NATS's `*` and `>` (`the tracking.> nats subject`). A `topic` or `subject` row then checks the one a message came on.

## How the checks listen

For the targets a run's checks name, axx starts listening once the services are up, before the first scenario, so no message is missed:

- **An AMQP queue** is read, and each message acknowledged, as any consumer would. Check the queues your services write to and nothing else reads.
- **An AMQP exchange** takes nothing from anyone. Axx declares a queue of its own for the run (`axx-<run>.<exchange>`, exclusive, deleted with the run), and binds it with `#` and with the routing keys the checks name. That covers topic, fanout, headers and direct exchanges.
- **An MQTT topic** gets a subscription of its own, on a connection of its own. It doesn't receive the retained messages the broker keeps for new subscribers, which earlier runs may have left.
- **A NATS subject** gets a subscription of its own.
- **A JetStream stream** is read with an ordered consumer of its own, from the messages stored after the services are up, so what earlier runs stored never counts. It takes nothing from the stream's other consumers. A work-queue stream allows no other consumer, so check its subject instead.

## Keep scenarios apart

Scenarios run in parallel on the same brokers, and every listener sees every scenario's messages. So a check tells a scenario's message apart by data unique to it, such as its own parcel reference ([Isolate test data](/guides/isolate-test-data/)). A queue the check reads is shared by the whole run: a message another scenario sent is still there for that scenario's check.

## Run the brokers locally

Start the brokers next to your service in the Compose file your [service definition](/guides/manage-services/) runs:

```yaml title="compose.yaml"
services:
  rabbitmq:
    image: rabbitmq:4.2.9-alpine
    ports: ['5672:5672']
    environment: { RABBITMQ_DEFAULT_USER: parcels, RABBITMQ_DEFAULT_PASS: parcels }
  mosquitto:
    image: eclipse-mosquitto:2.0.22
    command: ['mosquitto', '-c', '/mosquitto-no-auth.conf']
    ports: ['1883:1883']
  nats:
    image: nats:2.15.0-alpine
    command: ['nats-server', '-js']
    ports: ['4222:4222']
```

RabbitMQ's default `guest` user can only connect from the broker's own host, so give it a user of your own. Mosquitto only listens to its own host without a configuration of its own, and `/mosquitto-no-auth.conf` opens it up. NATS needs `-js` for JetStream.

See the [amqp](/references/packs/amqp/), [mqtt](/references/packs/mqtt/) and [nats](/references/packs/nats/) packs' references for every step.
