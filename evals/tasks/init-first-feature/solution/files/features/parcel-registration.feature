Feature: Parcel registration
  Shops register parcels by their own reference and look them up later.

  Background:
    Given the parcels service with the following properties:
      | url     | http://${sys:local.host}:8080              |
      | openapi | http://${sys:local.host}:8080/openapi.json |

  Scenario: A registered parcel can be looked up by its reference
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-FIRST-REG-01 |
      | sender    | shop-first-reg  |
    And a 2nd ordered GET request to /api/parcels/PX-FIRST-REG-01
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 200
    And the response payload properties for 2nd ordered response are:
      | reference | PX-FIRST-REG-01 |
      | sender    | shop-first-reg  |
      | status    | REGISTERED      |

  Scenario: The same reference cannot be registered twice
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-FIRST-DUP-01 |
      | sender    | shop-first-dup  |
    And a 2nd ordered POST request to /api/parcels
    And the request headers for 2nd ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 2nd ordered request
    And the request payload properties for 2nd ordered request are:
      | reference | PX-FIRST-DUP-01 |
      | sender    | shop-first-dup  |
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 409
