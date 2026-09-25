Feature: Shipment weights

  The parcels platform weighs every parcel and publishes a ShipmentWeighed event on the
  shipment-events topic. Billing keeps the weights to price the carriers' invoices.

  Background:
    Given the billing gcp project with the following properties:
      | project  | parcels-billing               |
      | endpoint | http://${sys:local.host}:4588 |
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |

  Scenario: A weighed parcel is kept for pricing
    When the messages/px-5105-weighed.json message is published to the shipment-events pubsub topic with the following attributes:
      | eventType | ShipmentWeighed |
    Then within 10s the shipments firestore collection has a document where:
      | parcel   | PX-5105 |
      | carrier  | HERON   |
      | weightKg | 3.2     |

  Scenario: A shipment event without a type is set aside
    When a message is published to the shipment-events pubsub topic:
      """
      {"parcel": "PX-5106", "carrier": "HERON", "service": "express", "weightKg": 1.1}
      """
    Then the console log has an entry matching 'shipment event ignored: no eventType.* parcel=PX-5106'
