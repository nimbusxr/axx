Feature: Depot scanners

  The depots' handheld scanners publish each scan over MQTT. The service records the
  scans of the parcels it knows, and alerts the depot when a scanner scans a parcel it
  does not know, so the depot finds the parcel before it goes any further.

  Background:
    Given the depots mqtt broker with the following properties:
      | url      | mqtt://${sys:local.host}:1883               |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |

  Scenario: A scanner's scan updates the parcel's tracking
    Given a seeds/scanner-scan.yaml db seed
    When a message is published to the depots/LEJ/scans mqtt topic:
      """
      {"scanId": "SC-8201-1", "parcelRef": "PX-MQT-8201", "status": "OUT_FOR_DELIVERY", "location": "Leipzig", "scannedAt": "2026-09-28T07:30:00Z"}
      """
    Then within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id          | PX-MQT-8201      |
      | status       | OUT_FOR_DELIVERY |
      | lastLocation | Leipzig          |

  Scenario: A scanner is alerted when it scans a parcel the service does not know
    When the mqtt/scan-unknown.json message is published to the depots/LEJ/scans mqtt topic with the following properties:
      | property scanner | LEJ-HANDHELD-7 |
    Then within 10s the depots/+/alerts mqtt topic has a message where:
      | topic            | depots/LEJ/alerts |
      | parcelRef        | PX-MQT-8299       |
      | problem          | unknown parcel    |
      | property scanner | LEJ-HANDHELD-7    |
