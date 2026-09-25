---
title: Test Kafka with Avro
description: Configure Kafka topic clients, build and publish Avro events with a Schema Registry, verify consumed events, and address several events or clusters in one scenario.
---

The Kafka steps publish the events your service consumes and check the events it produces, with Avro and a Schema Registry or as plain text.

## Register the cluster and a topic client

```gherkin
Background:
  Given the events kafka service with the following properties:
    | brokers | ${sys:local.host}:9092 |
  And a depot-scans kafka topic client with the following properties:
    | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
    | producer.schema.registry.url | http://${sys:local.host}:9081                      |
  And a parcel-events kafka topic client with the following properties:
    | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
    | consumer.schema.registry.url | http://${sys:local.host}:9081                        |
```

The service consumes depot scans, so the suite publishes to `depot-scans`; it announces registered parcels on `parcel-events`, so the suite reads that topic. A topic the suite both publishes to and reads takes both sets of properties.

The topic client's properties use the Kafka client property names, prefixed with `producer.` or `consumer.`. Axx translates them to its own Go client, so you do not need a JVM; the [Kafka step reference](/references/steps/kafka/) lists every property it understands. Consumer group settings (`group.id`, `enable.auto.commit`) have no effect, because Axx reads topics without a group.

For a TLS-secured cluster:

```gherkin
Given a secure-events kafka topic client with the following properties:
  | producer.security.protocol      | SASL_SSL                                                                                      |
  | producer.sasl.mechanism         | SCRAM-SHA-512                                                                                 |
  | producer.sasl.jaas.config       | org.apache.kafka.common.security.scram.ScramLoginModule required username="app" password="${env:KAFKA_PASSWORD}"; |
  | producer.ssl.truststore.location | certs/truststore.p12                                                                         |
  | producer.ssl.truststore.password | ${env:TRUSTSTORE_PASSWORD}                                                                   |
```

The consumer takes the same settings with the `consumer.` prefix.

For a topic on a different cluster, register that cluster and the client on it explicitly:

```gherkin
Given the depots kafka service with the following properties:
  | brokers | depots-kafka:9092 |
And a depot-scans kafka topic client on the depots kafka service with the following properties:
  | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
  | producer.schema.registry.url | http://depots-registry:8081                        |
```

## Build an event

```gherkin
Given a depot-scans kafka event
And the depot-scans kafka event key is PX-TRK-3002
And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
And the depot-scans kafka event payload properties are:
  | $.scanId    | SC-3002-2   |
  | $.parcelRef | PX-TRK-3002 |
And the depot-scans kafka event headers are:
  | X-Scanner-Id | leipzig-dock-4 |
```

- The payload starts from a file in Avro's JSON encoding. Union values are written as `{"string": "..."}` or `null`; set `packs.kafka.lenientUnions: true` in `axx.yaml` to accept bare values when only one branch fits.
- `the depot-scans kafka event payload properties are:` sets JSONPath properties of that topic's event; `the kafka event payload properties are:` uses the first topic client's. Values are always set as **strings** (an empty cell sets null), and each property must already exist in the payload, so give numbers and objects their values in the file.
- `the depot-scans kafka event payload property <path> is null` sets a property to JSON null (for an Avro union with `null`).
- Keep payload files schema-valid with [fixture factories](/guides/fixture-factories/) (the `avro` family); `kafka/scan-delivered.json` is generated that way.

## Publish

```gherkin
When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
```

The payload is read with the Avro schema (`.avsc`, resolved against `resources`), the schema is registered under `<topic>-value` (or looked up, with `producer.auto.register.schemas=false`), and the record is sent in the Confluent wire format with the key and headers you set. A payload that does not fit the schema fails with the path of the mismatch, such as `$.location: expected string, got number 5`.

To publish without Avro (plain text or JSON with the default `StringSerializer`):

```gherkin
Given a delivery-notifications kafka event
And the delivery-notifications kafka event payload is a kafka/delivery-notification.json resource
When the delivery-notifications kafka event is published
```

## Verify consumed events

Check the events your service publishes. After a scenario registers parcel `PX-EVT-4001` through the API:

```gherkin
Then the parcel-events kafka event named registered key is PX-EVT-4001
And the parcel-events kafka event named registered payload properties are:
  | $.reference   | PX-EVT-4001 |
  | $.weightGrams | 1200        |
  | $.zone        | DE-1        |
  | $.source      | api         |
And the parcel-events kafka event named registered headers are:
  | X-Event-Type | ParcelRegistered |
And the parcel-events kafka event named registered headers match:
  | X-Event-Type | ^Parcel.*$ |
```

`named registered` names an expectation. Each step adds its checks to the name and passes when one record of the topic meets all of them at once, so the four steps above check that a single record has that key, those properties and those headers. Records are read from the start of the topic (with `consumer.auto.offset.reset=latest`, only those produced after the step starts), and a step waits up to 30 seconds (`packs.kafka.timeout`). Expected payload values are typed, so `1200` does not equal `1200.0` ([the rules](/references/steps/kafka/#kafkaconsumedproperties)). `headers are` needs each header exactly once with that value; `headers match` takes regular expressions.

When a step fails, the report lists the name's checks and the latest records of the topic with the reason each one did not match.

## Test what your service does with an event

Publishing and then consuming on the same topic only checks the wiring. An acceptance test publishes the event your service consumes and checks what the service does: a row in its database, a document, a response from its API, or an event on another topic. The parcels service keeps a tracking summary in MongoDB for every parcel the depots scan:

```gherkin
Scenario: A delivery scan marks the parcel delivered
  Given a depot-scans kafka event
  And the depot-scans kafka event key is PX-TRK-3003
  And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
  And the depot-scans kafka event payload properties are:
    | $.scanId    | SC-3003-1   |
    | $.parcelRef | PX-TRK-3003 |
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then within 20s a selection of at least 1 document is retrieved from the tracking collection where:
    | _id       | PX-TRK-3003 |
    | delivered | true        |
  And the 1st document for the selection properties are:
    | status       | DELIVERED |
    | lastLocation | Leipzig   |
```

The `Background` registers the Kafka service, `the depot-scans kafka topic client` and the MongoDB database. The service handles the event in the background, so the `Then` step polls until the summary appears instead of sleeping.

## Several events in one scenario

Address events by ordinal when a scenario builds more than one. `a 2nd ordered ... kafka event` must follow the 1st; steps without an ordinal use the first event:

```gherkin
Given a 1st ordered depot-scans kafka event
And the 1st ordered depot-scans kafka event key is PX-TRK-3002
And the 1st ordered depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
And the 1st ordered depot-scans kafka event payload properties are:
  | $.scanId    | SC-3002-1   |
  | $.parcelRef | PX-TRK-3002 |
And a 2nd ordered depot-scans kafka event
And the 2nd ordered depot-scans kafka event key is PX-TRK-3002
And the 2nd ordered depot-scans kafka event payload is a kafka/scan-delivered.json resource
And the 2nd ordered depot-scans kafka event payload properties are:
  | $.scanId    | SC-3002-2   |
  | $.parcelRef | PX-TRK-3002 |
When the 1st ordered depot-scans kafka event is published using schema schemas/depot-scan.avsc
And the 2nd ordered depot-scans kafka event is published using schema schemas/depot-scan.avsc
```

## Several clusters

Every step has a form that names the Kafka service, such as `the depot-scans kafka event key is PX-TRK-3002 on the depots kafka service`. See the [step index](/references/step-index/) for the full list.

## Keep events apart

Topics keep their events across runs, and scenarios run in parallel. Use a key and identifiers that belong to one scenario (a parcel reference, a scan id), and assert on them. For fixture files, a generated `axx lint` rule can enforce that every event file owns a unique id, such as the `scanId` of every depot scan ([Isolate test data](/guides/isolate-test-data/)).
