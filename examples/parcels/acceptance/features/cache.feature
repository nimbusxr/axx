Feature: The cache

  The service keeps what it is asked for again and again in Valkey: a parcel's tracking
  view, until a scan changes it, and the parcel each registration made under its shop's
  idempotency key, so that a retried registration is answered, not registered again.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the cache redis server with the following properties:
      | url | redis://${sys:local.host}:6379/0 |

  Scenario: A parcel's tracking view is cached
    Given a seeds/cache-tracking.yaml db seed
    And a GET request to /api/parcels/PX-RDS-9601/tracking
    When the request is executed
    Then the response status code is 200
    And the tracking:PX-RDS-9601 redis key has the following properties:
      | parcelRef | PX-RDS-9601 |
      | status    | REGISTERED  |

  Scenario: A cached tracking view answers the tracking request
    Given a seeds/cache-cached.yaml db seed
    And a redis/tracking-PX-RDS-9602.yaml redis seed
    And a GET request to /api/parcels/PX-RDS-9602/tracking
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | status       | OUT_FOR_DELIVERY |
      | lastLocation | Leipzig          |

  Scenario: A scan drops the parcel's cached tracking view
    Given a seeds/cache-dropped.yaml db seed
    And a redis/tracking-PX-RDS-9603.yaml redis seed
    And the tracking:PX-RDS-9603 redis key has the following properties:
      | status | IN_TRANSIT |
    And the depots mqtt broker with the following properties:
      | url | mqtt://${sys:local.host}:1883 |
    When a message is published to the depots/LEJ/scans mqtt topic:
      """
      {"scanId": "SC-9603-1", "parcelRef": "PX-RDS-9603", "status": "OUT_FOR_DELIVERY", "location": "Leipzig", "scannedAt": "2026-09-28T07:45:00Z"}
      """
    Then within 20s the tracking:PX-RDS-9603 redis key does not exist

  Scenario: A registration retried with its idempotency key is answered with the parcel it made
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-RDS-9604'
    And the request payload property sender is 'rowanberry-crafts'
    And the request header Idempotency-Key is 'order-8812'
    And a 2nd ordered POST request to /api/parcels
    And a request payload using an application/json content example for 2nd ordered request
    And the request payload property reference is 'PX-RDS-9604' for 2nd ordered request
    And the request payload property sender is 'rowanberry-crafts' for 2nd ordered request
    And the request header Idempotency-Key is 'order-8812' for 2nd ordered request
    When the request is executed
    And the 2nd ordered request is executed
    Then the response status code is 201
    And the 2nd ordered response status code is 200
    And the response payload property reference is 'PX-RDS-9604' for 2nd ordered response
    And the idempotency:rowanberry-crafts:order-8812 redis key has the value 'PX-RDS-9604'
