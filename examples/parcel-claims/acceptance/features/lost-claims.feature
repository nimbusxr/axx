Feature: Claims for lost parcels

  A parcel claimed as lost is decided at once: paid out, unless the parcels platform
  reported it delivered. Deliveries come from the platform's parcel-events topic, typed
  by their eventType attribute.

  Background:
    Given the parcels aws account with the following properties:
      | region            | eu-west-1                           |
      | endpoint          | http://${sys:local.host}:4566       |
      | access key id     | ${env:AWS_ACCESS_KEY_ID:-local}     |
      | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |
    And the claims service with the following properties:
      | url     | http://${sys:local.host}:8500              |
      | openapi | http://${sys:local.host}:8500/openapi.yaml |
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |

  Scenario: A parcel that never arrived is paid out
    Given a seeds/PX-4103.yaml dynamodb seed
    And a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel      | PX-4103                          |
      | reason      | LOST                             |
      | description | Not delivered three weeks later |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | status | APPROVED |
      | amount | 45       |
    And the refund-requests sqs queue has a message where:
      | claim            | CLM-4103 |
      | amount           | 45       |
      | attribute reason | LOST     |

  Scenario: A parcel reported delivered is not paid out
    Given a seeds/PX-4104.yaml dynamodb seed
    When the messages/px-4104-delivered.json message is published to the parcel-events sns topic with the following attributes:
      | eventType | ParcelDelivered |
    Then within 10s the insured-parcels dynamodb table has an item where:
      | reference   | PX-4104              |
      | deliveredAt | 2026-09-20T14:05:00Z |
    Given a POST request to /api/claims
    And a request payload using an application/json content example
    And the request payload properties are:
      | parcel | PX-4104 |
      | reason | LOST    |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | status | REJECTED                        |
      | note   | delivered on 2026-09-20T14:05:00Z |
    And the claim-decisions sns topic has a message where:
      | claim    | CLM-4104 |
      | decision | REJECTED |

  Scenario: A parcel event without a type is set aside
    Given a seeds/PX-4108.yaml dynamodb seed
    When a message is published to the parcel-events sns topic:
      """
      {"parcel": "PX-4108", "deliveredAt": "2026-09-21T09:00:00Z"}
      """
    Then the console log has an entry matching 'parcel event ignored: no eventType.* parcel=PX-4108'
