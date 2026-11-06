# These scenarios make the service's connections fail for everyone, so they run alone
# (run.exclusiveUses in axx.yaml). The service reaches its database and its cache through
# Toxiproxy (../infra/compose.yaml); axx's own connections go straight to them.
Feature: Connections that fail

  The service reaches its database and its cache over the network, and a connection can
  fail in many ways. Whichever way it fails, a shop gets an answer: from the cache while
  the database is away, from the database while the cache is away, its parcel registered
  when the trouble is short, and a request to try again later when it lasts.

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
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And the parcels log with the following properties:
      | url | udp://0.0.0.0:5140 |
    And the parcels fault proxy with the following properties:
      | url | http://${sys:local.host}:8474 |

  Scenario: A cached tracking view answers while the database is cut off
    Given a redis/tracking-PX-FLT-9701.yaml redis seed
    And the parcels-db connection is cut off
    And a GET request to /api/parcels/PX-FLT-9701/tracking
    When the request is executed
    Then the response status code is 200
    And the response payload property status is 'OUT_FOR_DELIVERY'

  Scenario: Tracking answers from the database while the cache is cut off
    Given a seeds/faults-no-cache.yaml db seed
    And the cache connection is cut off
    And a GET request to /api/parcels/PX-FLT-9702/tracking
    When the request is executed
    Then the response status code is 200
    And the response payload property status is 'REGISTERED'
    And the parcels log has an entry matching 'msg="reading the tracking cache failed" reference=PX-FLT-9702'

  Scenario: A brief outage does not fail the registration
    Given the parcels-db connection is cut off for 1s
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-FLT-9703'
    When the request is executed
    Then the response status code is 201
    And the parcels log has an entry matching 'msg="the database did not answer, retrying" reference=PX-FLT-9703'
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-FLT-9703 |
    And the selection has 1 row

  Scenario: A connection that hangs and closes for a moment does not fail the registration
    Given the parcels-db connection hangs for 1s, then closes for 2s
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-FLT-9704'
    When the request is executed
    Then the response status code is 201
    And the response payload property reference is 'PX-FLT-9704'

  Scenario Outline: A database whose connection <fault> asks the shop to try again later
    Given the parcels-db connection <fault>
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is '<reference>'
    When the request is executed
    Then the response status code is 503
    And the response header Retry-After is '"5"'
    And the response payload property detail is 'the parcel could not be stored; try again later'

    Examples:
      | fault                  | reference   |
      | hangs                  | PX-FLT-9710 |
      | hangs going out        | PX-FLT-9711 |
      | hangs coming back      | PX-FLT-9712 |
      | is reset               | PX-FLT-9713 |
      | is reset after 1s      | PX-FLT-9714 |
      | closes after 100 bytes | PX-FLT-9715 |
      | loses 100% of its data | PX-FLT-9716 |

  Scenario: A shop's retry registers the parcel once the database is back
    Given the parcels-db connection is cut off
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-FLT-9720'
    And the request is executed
    And the response status code is 503
    When the parcels-db connection is restored
    And a 2nd ordered POST request to /api/parcels
    And a request payload using an application/json content example for 2nd ordered request
    And the 2nd ordered request payload property reference is 'PX-FLT-9720'
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 201
    And the 2nd ordered response payload property reference is 'PX-FLT-9720'

  Scenario Outline: A database whose connection <trouble> still registers the parcel
    Given the parcels-db connection <trouble>
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is '<reference>'
    When the request is executed
    Then the response status code is 201
    And the response payload property reference is '<reference>'

    Examples:
      | trouble                                            | reference   |
      | is slowed by 200ms                                 | PX-FLT-9730 |
      | is slowed by 100ms going out on 50% of connections | PX-FLT-9731 |
      | is limited to 100 KB/s                             | PX-FLT-9732 |
      | breaks into 100-byte pieces, 1ms apart             | PX-FLT-9733 |
      | takes 1s to close                                  | PX-FLT-9734 |
      | closes after 50 KB                                 | PX-FLT-9735 |

  Scenario: Latency that varies still registers the parcel
    Given the parcels-db connection has a latency toxic with the following attributes:
      | latency | 100 |
      | jitter  | 50  |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-FLT-9740'
    When the request is executed
    Then the response status code is 201
    And the response payload property reference is 'PX-FLT-9740'
