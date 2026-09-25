Feature: Tracking

  Depot scanners publish a scan every time a parcel passes a depot. Scans arrive late,
  out of order and sometimes twice; the tracking summary always shows the latest scan
  and counts every scan once.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://localhost:27017/parcels?authSource=admin |
      | user     | parcels                                            |
      | password | parcels                                            |
    And the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a depot-scans kafka topic client with the following properties:
      | producer.value.serializer    | io.confluent.kafka.serializers.KafkaAvroSerializer |
      | producer.schema.registry.url | http://${sys:local.host}:9081                      |

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
