Feature: Tracking updates

  Couriers confirm each delivery from their handhelds over NATS. The service tells the
  other services of every scan it records, from the depots' systems, their scanners or
  the couriers, on tracking.<reference>, and the TRACKING stream keeps the updates for
  the services that catch up later.

  Background:
    Given the tracking nats server with the following properties:
      | url | nats://${sys:local.host}:4222 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |

  Scenario: A courier's confirmation marks the parcel delivered
    Given a seeds/courier-delivered.yaml db seed
    When a message is published to the deliveries.confirmed nats subject:
      """
      {"reference": "PX-NAT-8301", "location": "Leipzig", "deliveredAt": "2026-09-28T10:12:00Z", "signedBy": "H. Wolf"}
      """
    Then within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-NAT-8301 |
      | status    | DELIVERED   |
      | delivered | true        |

  Scenario: The other services hear of a depot's scan
    Given a seeds/tracking-fanout.yaml db seed
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-NAT-8302
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-8302-1   |
      | $.parcelRef | PX-NAT-8302 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking.> nats subject has a message where:
      | subject       | tracking.PX-NAT-8302 |
      | status        | OUT_FOR_DELIVERY     |
      | header source | depot-system         |

  Scenario: The TRACKING stream keeps a courier's confirmation, with the courier
    Given a seeds/courier-stream.yaml db seed
    When the nats/PX-NAT-8303-delivered.json message is published to the deliveries.confirmed nats subject with the following headers:
      | courier | CR-LEJ-12 |
    Then within 20s the TRACKING nats stream has a message where:
      | subject        | tracking.PX-NAT-8303 |
      | status         | DELIVERED            |
      | header source  | courier              |
      | header courier | CR-LEJ-12            |
