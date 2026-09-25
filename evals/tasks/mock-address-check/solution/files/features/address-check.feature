Feature: Address check at registration
  Before registering a parcel, the service asks the address service whether we deliver to
  the recipient's postcode, and stores the delivery zone it returns.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8080              |
      | openapi | http://localhost:8080/openapi.json |
    And the mocked address-service service with the following properties:
      | url | http://address-service:8080 |

  Scenario: Registering a parcel checks the postcode with our API key
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADDR-CHECK-001 |
      | sender             | shop-addr-check   |
      | recipient.postcode | 10317             |
      | recipient.country  | DE                |
    When the request is executed
    Then the response status code is 201
    And the mocked GET request to /v1/postcodes/DE/10317 named postcode-10317 was received by address-service
    And the mocked request named postcode-10317 was received exactly 1 time
    And the header X-Api-Key for mocked request named postcode-10317 is 'evals-address-key'

  Scenario: The parcel carries the delivery zone from the address service
    Given a POST request to /api/parcels
    And the request headers are:
      | Content-Type | application/json |
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADDR-ZONE-001 |
      | sender             | shop-addr-zone   |
      | recipient.city     | Lyon             |
      | recipient.postcode | 69002            |
      | recipient.country  | FR               |
    When the request is executed
    Then the response status code is 201
    And the response payload property zone is 'FR-1'

  Scenario: An undeliverable postcode is refused and nothing is stored
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference          | PX-ADDR-NODELIVERY-001 |
      | sender             | shop-addr-nodelivery   |
      | recipient.postcode | 99920                  |
      | recipient.country  | DE                     |
    And a 2nd ordered GET request to /api/parcels/PX-ADDR-NODELIVERY-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 422
    And the 2nd ordered response status code is 404
    And the mocked GET request to /v1/postcodes/DE/99920 named postcode-99920 was received by address-service
