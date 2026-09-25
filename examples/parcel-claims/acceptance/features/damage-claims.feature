Feature: Claims for damaged parcels

  A shop claims for an insured parcel that arrived damaged, then uploads a photo of the
  damage. A claim within the auto-approval limit is settled as soon as the photo arrives:
  the shop gets a letter, payments get a refund request, and the decision is announced. A
  larger claim goes to a person, with the photo copied for the reviewer.

  Background:
    Given the parcels aws account with the following properties:
      | region            | eu-west-1                           |
      | endpoint          | http://${sys:local.host}:4566       |
      | access key id     | ${env:AWS_ACCESS_KEY_ID:-local}     |
      | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |
    And the claims service with the following properties:
      | url     | http://${sys:local.host}:8500              |
      | openapi | http://${sys:local.host}:8500/openapi.yaml |

  Scenario: A claim within the limit is settled when its photo arrives
    Given a seeds/PX-4101.yaml dynamodb seed
    And a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel | PX-4101 |
      | reason | DAMAGED |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | id     | CLM-4101          |
      | status | AWAITING_EVIDENCE |
    And the parcels eventbridge bus has an event where:
      | detail-type   | Claim Filed |
      | source        | parcels.claims |
      | detail.claim  | CLM-4101    |
      | detail.reason | DAMAGED     |
    When the evidence/crushed-box.png file is uploaded to the claim-evidence s3 bucket as claims/CLM-4101/crushed-box.png
    Then within 30s the claims dynamodb table has an item where:
      | id     | CLM-4101 |
      | status | APPROVED |
      | amount | 89.5     |
    And the CLM-4101.json object in the claim-letters s3 bucket has the following properties:
      | decision | APPROVED  |
      | amount   | 89.5      |
      | currency | EUR       |
      | shop     | ACME-TOYS |
    And the refund-requests sqs queue has a message where:
      | claim            | CLM-4101  |
      | shop             | ACME-TOYS |
      | amount           | 89.5      |
      | attribute reason | DAMAGED   |
    And the claim-decisions sns topic has a message where:
      | claim               | CLM-4101     |
      | decision            | APPROVED     |
      | attribute eventType | ClaimDecided |

  Scenario: A claim above the limit goes to a person with its photo
    Given a seeds/PX-4102.yaml dynamodb seed
    And a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel | PX-4102 |
      | reason | DAMAGED |
    When the request is executed
    Then the response status code is 201
    When the evidence/torn-parcel.png file is uploaded to the claim-evidence s3 bucket as claims/CLM-4102/torn-parcel.png
    Then within 30s the claim-reviews s3 bucket has an object named CLM-4102/torn-parcel.png
    And the CLM-4102/torn-parcel.png object in the claim-reviews s3 bucket is identical to the evidence/torn-parcel.png file
    And the claims dynamodb table has an item where:
      | id     | CLM-4102 |
      | status | REFERRED |
    And the parcels eventbridge bus has an event where:
      | detail-type     | Claim Decided |
      | detail.claim    | CLM-4102      |
      | detail.decision | REFERRED      |

  Scenario: A parcel is claimed once
    Given a seeds/PX-4107.yaml dynamodb seed
    And a 1st ordered POST request to /api/claims
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | parcel | PX-4107 |
      | reason | DAMAGED |
    And a 2nd ordered POST request to /api/claims
    And a request payload using an application/json content example for 2nd ordered request
    And the request payload properties for 2nd ordered request are:
      | parcel | PX-4107 |
      | reason | LOST    |
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 409
    And the claims dynamodb table has 1 item where:
      | parcel | PX-4107 |
