Feature: Shipment weights

  The parcels platform weighs every parcel and publishes a ShipmentWeighed event on the
  shipment-events topic. Billing keeps the weights to price the carriers' invoices. When
  the platform cancels a parcel, it publishes a ShipmentCancelled event, and billing no
  longer keeps the parcel: a carrier that bills it is disputed.

  Background:
    Given the billing gcp project with the following properties:
      | project  | parcels-billing               |
      | endpoint | http://${sys:local.host}:4588 |
    And the console log with the following properties:
      | url | file://.axx/logs/services.log |

  Scenario: A weighed parcel is kept for pricing
    When the messages/px-5105-weighed.json message is published to the shipment-events pubsub topic with the following attributes:
      | eventType | ShipmentWeighed |
    Then within 10s the shipments firestore collection has a document where:
      | parcel    | PX-5105              |
      | carrier   | HERON                |
      | weightKg  | 3.2                  |
      | weighedAt | 2026-09-03T14:05:00Z |

  Scenario: A cancelled parcel is no longer kept for pricing
    Given a seeds/px-5108-weighed.yaml firestore seed
    When the messages/px-5108-cancelled.json message is published to the shipment-events pubsub topic with the following attributes:
      | eventType | ShipmentCancelled |
    Then within 10s the shipments firestore collection has no document where:
      | parcel | PX-5108 |
    And the console log has an entry matching 'shipment cancelled.* parcel=PX-5108'

  Scenario: A shipment event without a type is set aside
    When a message is published to the shipment-events pubsub topic:
      """
      {"parcel": "PX-5106", "carrier": "HERON", "service": "express", "weightKg": 1.1}
      """
    Then the console log has an entry matching 'shipment event ignored: no eventType.* parcel=PX-5106'
