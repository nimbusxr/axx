Feature: Address check

  Every registration asks the address service whether the recipient's postcode is
  deliverable and which delivery zone serves it. The address service expects the
  parcels service's API key on every call.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the parcels log with the following properties:
      | url | udp://0.0.0.0:5140 |
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |

  Scenario: The address service is asked with the API key
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADR-1101 |
      | recipient.postcode | "53111"     |
    When the request is executed
    Then the response status code is 201
    And the mocked GET request to /v1/postcodes/DE/53111 named postcode-check was received by addresses
    And the headers for mocked request named postcode-check on addresses are:
      | X-Api-Key | example-address-key |
      | Accept    | application/json    |

  Scenario: Parcels to undeliverable postcodes are refused
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADR-1102 |
      | recipient.postcode | "99990"     |
    When the request is executed
    Then the response status code is 422
    And the response payload property detail is 'address not deliverable: no delivery to this postcode'
    And the mocked GET request to /v1/postcodes/DE/99990 named undeliverable-check was received by addresses
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-ADR-1102 |
    And the selection has 0 rows

  Scenario: Invalid registrations never reach the address service
    Given the OpenAPI validation levels are:
      | validation.request.body.schema.minimum | IGNORE |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADR-1103 |
      | weightGrams        | 0           |
      | recipient.postcode | "12489"     |
    When the request is executed
    Then the response status code is 400
    And the mocked GET request to /v1/postcodes/DE/12489 named skipped-check was not received

  Scenario: Registration is refused while the address service is unavailable
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADR-1104 |
      | recipient.postcode | "00012"     |
    When the request is executed
    Then the response status code is 502
    And the response payload property detail is 'the address service is unavailable; try again later'
    And the logs have entries matching:
      | parcels | msg="registration refused; not announced" reference=PX-ADR-1104 reason="the address service is unavailable" |
      | console | Request received:\n.*GET /v1/postcodes/DE/00012                                                              |
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-ADR-1104 |
    And the selection has 0 rows
