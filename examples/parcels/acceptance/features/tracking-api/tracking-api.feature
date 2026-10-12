Feature: Tracking API

  The shops' own systems follow their parcels through the service's tracking API, over
  gRPC: a parcel's status and latest scan, and a stream of its scans as they happen, which
  ends once the parcel is delivered. The API serves its descriptors through gRPC server
  reflection, which is how the steps read its messages.

  Background:
    Given the tracking grpc service with the following properties:
      | address | ${sys:local.host}:8410 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the events kafka service with the following properties:
      | brokers  | ${sys:local.host}:9092                      |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |

  Scenario: A shop's system reads a parcel's status
    Given a seeds/grpc-parcel.yaml db seed
    When the parcels.tracking.v1.Tracking/GetParcel method is called on the tracking grpc service with the following fields:
      | reference | PX-GRP-6101 |
    Then the tracking grpc service answered OK
    And the tracking grpc service's answer has the following fields:
      | reference    | PX-GRP-6101 |
      | status       | REGISTERED  |
      | serviceLevel | EXPRESS     |
      | weightGrams  | 2100        |
      | lastScan     | undefined   |

  Scenario: The tracking API does not know a parcel that was never registered
    When the Tracking/GetParcel method is called on the tracking grpc service with the following fields:
      | reference | PX-GRP-6199 |
    Then the tracking grpc service answered NOT_FOUND with a message containing 'PX-GRP-6199'

  Scenario: A shop's system watches its parcel go out for delivery
    Given a seeds/grpc-watch.yaml db seed
    And the Tracking/WatchParcel method is called on the tracking grpc service with the following fields:
      | reference | PX-GRP-6102 |
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-GRP-6102
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-6102-1   |
      | $.parcelRef | PX-GRP-6102 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking grpc service streamed a message where:
      | reference | PX-GRP-6102      |
      | status    | OUT_FOR_DELIVERY |
      | location  | Leipzig          |

  Scenario: The stream ends once the parcel is delivered
    Given a seeds/grpc-delivered.yaml db seed
    And the Tracking/WatchParcel method is called on the tracking grpc service with the following fields:
      | reference | PX-GRP-6103 |
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-GRP-6103
    And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-6103-1   |
      | $.parcelRef | PX-GRP-6103 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking grpc service streamed a message where:
      | reference | PX-GRP-6103 |
      | status    | DELIVERED   |
    And within 10s the tracking grpc service answered OK
