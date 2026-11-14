Feature: Tracking

  Depot scanners publish a scan every time a parcel passes a depot. Scans arrive late,
  out of order and sometimes twice; the tracking summary always shows the latest scan
  and counts every scan once. When a scan says a parcel was delivered, the service tells
  the parcel's shop in a webhook.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://localhost:27017/parcels?authSource=admin |
      | user     | parcels                                            |
      | password | parcels                                            |
    And the events kafka service with the following properties:
      | brokers  | ${sys:local.host}:9092                      |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the mocked shops service with the following properties:
      | url | http://${sys:local.host}:8083 |

  Scenario: Tracking shows the latest scan, however the scans arrive
    Given a seeds/scans-in-transit.json mongo db seed
    And within 10s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-TRK-3001 |
      | scanCount | 2           |
    When a GET request to /api/parcels/PX-TRK-3001/tracking
    And the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | status       | IN_TRANSIT  |
      | lastLocation | Hamburg hub |
      | scanCount    | 2           |
      | delivered    | false       |

  Scenario: A delivery scan marks the parcel delivered
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
    Then within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-TRK-3002 |
      | scanCount | 2           |
    And the 1st document for the selection properties are:
      | status       | DELIVERED |
      | lastLocation | Leipzig   |
      | delivered    | true      |

  Scenario: Every scan a depot publishes is taken into the parcel's tracking
    Given a depot-scans kafka event
    And the depot-scans kafka event key is PX-TRK-3005
    And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-3005-1   |
      | $.parcelRef | PX-TRK-3005 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s a selection of at least 1 document is retrieved from the scans collection where:
      | parcelRef | PX-TRK-3005 |
    And within 10s the scans collection has no documents where:
      | parcelRef | PX-TRK-3005 |
      | projected | undefined   |
    And a 2nd selection of documents is retrieved from the tracking collection where:
      | _id    | PX-TRK-3005      |
      | status | OUT_FOR_DELIVERY |
    And the 2nd selection has 1 document

  Scenario: The shop hears of a delivery the depot scans
    Given a seeds/tracking-delivered.yaml db seed
    And a depot-scans kafka event
    And the depot-scans kafka event key is PX-TRK-3004
    And the depot-scans kafka event payload is a kafka/scan-delivered.json resource
    And the depot-scans kafka event payload properties are:
      | $.scanId    | SC-3004-1   |
      | $.parcelRef | PX-TRK-3004 |
    When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
    Then within 20s the mocked POST request to path /webhooks/fennel-and-flint named delivered was received by shops
    And the payload properties for mocked request named delivered on shops are:
      | type      | parcel.delivered |
      | reference | PX-TRK-3004      |
    And the mocked request named delivered on shops is signed as a standard webhook with the key '${sys:shops.webhook-key}'
