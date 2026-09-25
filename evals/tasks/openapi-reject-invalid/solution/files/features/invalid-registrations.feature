Feature: Invalid registrations are refused
  Parcels we can't carry are refused at registration, before anything is stored.
  These requests break the published contract on purpose, so each scenario relaxes
  the client-side check of the request body.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8080              |
      | openapi | http://localhost:8080/openapi.json |
    And the OpenAPI validation levels are:
      | validation.request.body | IGNORE |

  Scenario: A parcel heavier than 30 kg is refused
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference   | PX-INV-HEAVY-001 |
      | sender      | shop-inv-heavy   |
      | weightGrams | 30001            |
    When the request is executed
    Then the response status code is 400
    And the response header Content-Type is 'application/problem+json'
    And the response payload property detail matches ^.*weightGrams.*$

  Scenario: A service level we don't offer is refused
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference    | PX-INV-LEVEL-001 |
      | sender       | shop-inv-level   |
      | serviceLevel | OVERNIGHT        |
    When the request is executed
    Then the response status code is 400
    And the response payload property detail matches ^.*serviceLevel.*$

  Scenario: A refused parcel is not registered
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference   | PX-INV-NOTSTORED-001 |
      | sender      | shop-inv-notstored   |
      | weightGrams | 45000                |
    And a 2nd ordered GET request to /api/parcels/PX-INV-NOTSTORED-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 400
    And the 2nd ordered response status code is 404
