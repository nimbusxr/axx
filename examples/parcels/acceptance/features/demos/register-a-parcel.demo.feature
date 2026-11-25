# demo: [card 3s] Registering a parcel
Feature: Register a parcel, as a demo

  A demo of what happens when a shop registers a parcel: `axx demo` runs the scenario and
  films what it did. `axx run` runs it as any other feature. The comments that start with
  "demo:" are what the people who watch the film read.

  # demo: This is our parcels system. Watch what happens across it
  #       when a shop registers a single parcel.
  Scenario: A shop registers a parcel, and everyone who has to know of it does
    # demo: [cut]
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a parcel-events kafka topic client with the following properties:
      | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
      | consumer.schema.registry.url | http://${sys:local.host}:9081                        |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-DEMO-9001 |
      | recipient.postcode | "10115"      |

    # demo: [action] [as shop] The shop sends us the parcel's details.
    When the request is executed

    # demo: We answer that it is registered,
    Then the response status code is 201

    # demo: after asking the address service whether we deliver to that postcode.
    And the mocked GET request to /v1/postcodes/DE/10115 named postcode-check was received by addresses

    # demo: [diagram stored + announced] The depots' database has the parcel,
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-DEMO-9001 |

    # demo: [pause 2s] [parcel-events --> billing] and billing is told of it, with an event.
    And the parcel-events kafka event named registered key is PX-DEMO-9001
