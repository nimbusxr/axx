Feature: Refunds

  Payments pays the refunds of approved claims and reports back on the refund-results
  queue: paid, or failed with the reason in a message attribute.

  Background:
    Given the parcels aws account with the following properties:
      | region            | eu-west-1                           |
      | endpoint          | http://${sys:local.host}:4566       |
      | access key id     | ${env:AWS_ACCESS_KEY_ID:-local}     |
      | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |
    And the claims service with the following properties:
      | url     | http://${sys:local.host}:8500              |
      | openapi | http://${sys:local.host}:8500/openapi.yaml |

  Scenario: A paid refund closes the claim
    Given a seeds/PX-4106.yaml dynamodb seed
    And a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel | PX-4106 |
      | reason | LOST    |
    When the request is executed
    Then the response status code is 201
    When a message is sent to the refund-results sqs queue:
      """
      {"claim": "CLM-4106", "status": "PAID", "paidAt": "2026-09-24T10:00:00Z"}
      """
    Then within 10s the claims dynamodb table has an item where:
      | id     | CLM-4106             |
      | status | PAID                 |
      | paidAt | 2026-09-24T10:00:00Z |

  Scenario: A failed refund is flagged with its reason
    Given a seeds/PX-4109.yaml dynamodb seed
    And a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel | PX-4109 |
      | reason | LOST    |
    When the request is executed
    Then the response status code is 201
    When the messages/refund-failed-clm-4109.json message is sent to the refund-results sqs queue with the following attributes:
      | failureReason | ACCOUNT_CLOSED |
    Then within 10s the claims dynamodb table has an item where:
      | id     | CLM-4109       |
      | status | REFUND_FAILED  |
      | note   | ACCOUNT_CLOSED |
