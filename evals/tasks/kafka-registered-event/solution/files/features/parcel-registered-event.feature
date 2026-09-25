Feature: ParcelRegistered events
  Every registered parcel is announced on the parcel-events topic, so billing and the
  depots can plan with it.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8080              |
      | openapi | http://localhost:8080/openapi.json |
    And the parcels-kafka kafka service with the following properties:
      | brokers | kafka:9092 |
    And a parcel-events kafka topic client with the following properties:
      | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
      | consumer.schema.registry.url | http://schema-registry:8081                          |
      | consumer.auto.offset.reset   | earliest                                             |

  Scenario: Registering a parcel publishes a ParcelRegistered event
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-EVENT-API-01 |
      | sender             | shop-event-api  |
      | weightGrams        | 2750            |
      | serviceLevel       | EXPRESS         |
      | recipient.postcode | 04109           |
      | recipient.country  | DE              |
    When the request is executed
    Then the response status code is 201
    And the parcel-events kafka event named registered key is PX-EVENT-API-01
    And the parcel-events kafka event named registered payload properties are:
      | $.reference    | PX-EVENT-API-01 |
      | $.sender       | shop-event-api  |
      | $.weightGrams  | 2750            |
      | $.serviceLevel | EXPRESS         |
      | $.zone         | DE-1            |
      | $.source       | api             |
    And the parcel-events kafka event named registered headers are:
      | X-Event-Type | ParcelRegistered |
