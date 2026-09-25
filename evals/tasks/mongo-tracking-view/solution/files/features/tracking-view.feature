Feature: Tracking view
  Depot scanners record scans; each parcel's tracking view shows where it is now.

  Background:
    Given a parcels-mongo mongo database with the following properties:
      | url      | mongodb://mongo:27017/parcels?authSource=admin |
      | user     | parcels                                        |
      | password | parcels                                        |

  Scenario: Tracking shows the most recent scan even when scans arrive out of order
    Given a seeds/tracking-out-of-order.json mongo db seed
    Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:
      | parcelRef | PX-TRK-ORDER-01 |
      | scanCount | 3               |
    And the document selection has 1 document
    And the 1st document of the document selection has the following properties:
      | status       | OUT_FOR_DELIVERY |
      | lastLocation | Leipzig depot    |
      | delivered    | false            |

  Scenario: A scan sent twice is counted once
    Given a seeds/tracking-resent.json mongo db seed
    Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:
      | parcelRef    | PX-TRK-RESENT-01 |
      | lastLocation | Kiel hub         |
    And the document selection has 1 document
    And the 1st document of the document selection has the following properties:
      | status    | IN_TRANSIT |
      | scanCount | 2          |

  Scenario: A delivered parcel is shown as delivered
    Given a seeds/tracking-delivered.json mongo db seed
    Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:
      | parcelRef | PX-TRK-DELIVERED-01 |
      | delivered | true                |
    And the document selection has 1 document
    And the 1st document of the document selection has the following properties:
      | status       | DELIVERED  |
      | lastLocation | Front door |
