Feature: Damage reported by carriers

  Carriers put a "Damage Reported" event on their bus when they damage a parcel in
  their care. The carrier admits it, so the claim is settled without a photo.

  Background:
    Given the parcels aws account with the following properties:
      | region            | eu-west-1                           |
      | endpoint          | http://${sys:local.host}:4566       |
      | access key id     | ${env:AWS_ACCESS_KEY_ID:-local}     |
      | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |

  Scenario: A carrier's damage report settles the claim
    Given a seeds/PX-4105.yaml dynamodb seed
    When a "Damage Reported" event from carrier.kestrel is put on the carrier-events eventbridge bus:
      """
      {"parcel": "PX-4105", "carrier": "KESTREL", "note": "Forklift damage at the Leeds depot"}
      """
    Then within 30s the claims dynamodb table has an item where:
      | id     | CLM-4105        |
      | status | APPROVED        |
      | source | CARRIER:KESTREL |
      | amount | 75              |
    And the refund-requests sqs queue has a message where:
      | claim  | CLM-4105 |
      | amount | 75       |
    And the parcels eventbridge bus has an event where:
      | detail-type     | Claim Decided |
      | detail.claim    | CLM-4105      |
      | detail.decision | APPROVED      |
