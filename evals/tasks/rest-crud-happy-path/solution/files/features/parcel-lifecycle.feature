Feature: Parcel lifecycle
  Shops register parcels, look them up, change them before pickup and cancel them.

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
      | reference          | PX-LIFE-REG-001 |
      | sender             | shop-life-reg   |
      | weightGrams        | 1500            |
      | serviceLevel       | EXPRESS         |
      | recipient.name     | Ada Register    |
      | recipient.postcode | 10115           |
      | recipient.country  | DE              |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | reference          | PX-LIFE-REG-001 |
      | status             | REGISTERED      |
      | weightGrams        | 1500            |
      | serviceLevel       | EXPRESS         |
      | recipient.name     | Ada Register    |
      | recipient.postcode | "10115"         |
      | recipient.country  | DE              |

  Scenario: A registered parcel can be looked up by its reference
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference   | PX-LIFE-GET-001 |
      | sender      | shop-life-get   |
      | weightGrams | 2200            |
    And a 2nd ordered GET request to /api/parcels/PX-LIFE-GET-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 200
    And the response payload properties for 2nd ordered response are:
      | reference   | PX-LIFE-GET-001 |
      | sender      | shop-life-get   |
      | status      | REGISTERED      |
      | weightGrams | 2200            |

  Scenario: A changed weight is kept
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference   | PX-LIFE-CHG-001 |
      | sender      | shop-life-chg   |
      | weightGrams | 1000            |
    And a 2nd ordered PATCH request to /api/parcels/PX-LIFE-CHG-001
    And the request headers for 2nd ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json empty content template for 2nd ordered request
    And the request payload property weightGrams is '3100' for 2nd ordered request
    And a 3rd ordered GET request to /api/parcels/PX-LIFE-CHG-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    And the 3rd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 200
    And the 3rd ordered response status code is 200
    And the response payload property weightGrams is '3100' for 3rd ordered response

  Scenario: A cancelled parcel can no longer be found
    Given a 1st ordered POST request to /api/parcels
    And the request headers for 1st ordered request are:
      | Content-Type | application/json |
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-LIFE-DEL-001 |
      | sender    | shop-life-del   |
    And a 2nd ordered DELETE request to /api/parcels/PX-LIFE-DEL-001
    And a 3rd ordered GET request to /api/parcels/PX-LIFE-DEL-001
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    And the 3rd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 204
    And the 3rd ordered response status code is 404
