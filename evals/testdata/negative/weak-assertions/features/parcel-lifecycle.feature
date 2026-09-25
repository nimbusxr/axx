Feature: Parcel lifecycle
  A weak answer: it registers parcels and checks status codes, but never looks at what the
  change or the cancellation did.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8080              |
      | openapi | http://localhost:8080/openapi.json |

  Scenario: A shop registers a parcel
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference | PX-WEAK-REG-001 |
      | sender    | shop-weak-reg   |
    When the request is executed
    Then the response status code is 201

  Scenario: A registered parcel can be looked up
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-WEAK-GET-001 |
      | sender    | shop-weak-get   |
    And a 2nd ordered GET request to /api/parcels/PX-WEAK-GET-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200

  Scenario: A shop changes and cancels a parcel
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-WEAK-CHG-001 |
      | sender    | shop-weak-chg   |
    And a 2nd ordered PATCH request to /api/parcels/PX-WEAK-CHG-001
    And the request headers for 2nd ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json empty content template for 2nd ordered request
    And the request payload property weightGrams is '3100' for 2nd ordered request
    And a 3rd ordered DELETE request to /api/parcels/PX-WEAK-CHG-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    And the 3rd ordered request is executed
    Then the 2nd ordered response status code is 200
    And the 3rd ordered response status code is 204
