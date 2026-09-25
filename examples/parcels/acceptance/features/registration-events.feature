Feature: Registration events

  Every registered parcel, whether it came through the API or a manifest, is announced
  as a ParcelRegistered event keyed by its reference, so billing and the depots can
  prepare for it.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a parcel-events kafka topic client with the following properties:
      | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
      | consumer.schema.registry.url | http://${sys:local.host}:9081                        |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And the parcels log with the following properties:
      | url | udp://0.0.0.0:5140 |

  Scenario: Registering a parcel announces it
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-EVT-4001'
    When the request is executed
    Then the response status code is 201
    And the parcel-events kafka event named registered key is PX-EVT-4001
    And the parcel-events kafka event named registered payload properties are:
      | $.reference    | PX-EVT-4001 |
      | $.weightGrams  | 1200        |
      | $.serviceLevel | STANDARD    |
      | $.zone         | DE-1        |
      | $.source       | api         |
    And the parcel-events kafka event named registered headers are:
      | X-Event-Type | ParcelRegistered |

  Scenario: A refused registration is not announced
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-EVT-4003 |
      | recipient.postcode | "99991"     |
    When the request is executed
    Then the response status code is 422
    And the parcels log has an entry matching 'msg="registration refused; not announced" reference=PX-EVT-4003 reason="address not deliverable'

  Scenario: Imported parcels are announced too
    When a seeds/manifest-maple.yaml db seed
    Then the parcel-events kafka event named imported key is PX-MAP-2001
    And the parcel-events kafka event named imported payload properties are:
      | $.source | manifest   |
      | $.sender | maple-home |
