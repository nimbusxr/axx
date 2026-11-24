# demo: [card 3s] Registering a parcel in the portal
Feature: Register a parcel in the portal, as a demo

  A demo that goes from a browser to what has no screen: a shop registers a parcel in the
  portal's form, and the depots' database and billing know of it. `axx demo` films the
  browser while the scenario is in it, and a diagram of what the scenario saw afterwards.

  Scenario: A shop registers a parcel in the portal, and the depots and billing know of it
    # demo: [cut]
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a parcel-events kafka topic client with the following properties:
      | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
      | consumer.schema.registry.url | http://${sys:local.host}:9081                        |
    And the "/parcels/new" page is opened

    # demo: [action] A shop with no system of its own registers its parcels in the portal.
    When the "Shop account" field is filled with "fjord-outdoor"
    And the "Parcel reference" field is filled with "PX-DEMO-9002"
    And the "Weight (grams)" field is filled with '"1200"'

    # demo: It says who the parcel is for, and where it goes.
    And the "Recipient name" field is filled with "Anna Weber"
    And the "Street" field is filled with "Invalidenstr. 116"
    And the "Postcode" field is filled with '"10115"'
    And the "City" field is filled with "Berlin"
    And "Germany" is chosen in the "Destination country" field

    # demo: [pause 2s] Then it registers the parcel.
    And the "Register the parcel" button is clicked
    Then the page shows "Parcel PX-DEMO-9002 registered"

    # demo: [show browser + diagram] The depots' database has the parcel,
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-DEMO-9002 |

    # demo: [pause 2s] and billing is told of it, as of any other parcel.
    And the parcel-events kafka event named registered key is PX-DEMO-9002
