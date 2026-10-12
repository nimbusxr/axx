Feature: Live tracking

  A parcel's depot scans reach those who follow it as they happen: the tracking page over a
  websocket, and the shops' own systems over an event stream, which ends once the parcel is
  delivered.

  Background:
    Given a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the events kafka service with the following properties:
      | brokers  | ${sys:local.host}:9092                      |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |

  Scenario: The tracking page hears that its parcel is out for delivery
    Given a seeds/live-websocket.yaml db seed
    And the tracking websocket with the following properties:
      | url      | ws://${sys:local.host}:8400/portal/track/PX-LIV-5701/live |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml               |
    And a message is sent to the tracking websocket:
      """
      {"follow": "PX-LIV-5701"}
      """
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-LIV-5701
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-5701-1   |
      | $.parcelRef | PX-LIV-5701 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking websocket received a message where:
      | reference | PX-LIV-5701      |
      | status    | OUT_FOR_DELIVERY |
      | location  | Leipzig          |

  Scenario: The tracking websocket closes a connection that does not say which parcel it follows
    Given the AsyncAPI validation levels are:
      | validation.message.payload | IGNORE |
    And the tracking websocket with the following properties:
      | url      | ws://${sys:local.host}:8400/portal/track/PX-LIV-5702/live |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml               |
    When a message is sent to the tracking websocket:
      """
      {"parcel": "PX-LIV-5702"}
      """
    Then the tracking websocket was closed with code 1008

  Scenario: A shop's system hears each depot scan
    Given a seeds/live-events.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-LIV-5703/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-LIV-5703
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-5703-1   |
      | $.parcelRef | PX-LIV-5703 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking event stream has an event where:
      | event type | scan             |
      | reference  | PX-LIV-5703      |
      | status     | OUT_FOR_DELIVERY |
      | location   | Leipzig          |

  Scenario: A shop's system hears that the parcel is delivered
    Given a seeds/live-delivered.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-LIV-5704/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-LIV-5704
    And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-5704-1   |
      | $.parcelRef | PX-LIV-5704 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the tracking event stream has an event where:
      | event type | delivered   |
      | event id   | 1           |
      | reference  | PX-LIV-5704 |
      | status     | DELIVERED   |
