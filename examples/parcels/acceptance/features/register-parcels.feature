Feature: Register parcels

  Shops register every parcel before they hand it to the depot. A registered parcel can
  be looked up, changed and cancelled until it is picked up.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |

  Scenario: A shop registers a parcel
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-REG-1001'
    When the request is executed
    Then the response status code is 201
    And the response header Location is '/api/parcels/PX-REG-1001'
    And the response payload properties are:
      | reference | PX-REG-1001 |
      | status    | REGISTERED  |
      | zone      | DE-1        |
      | source    | api         |
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-REG-1001 |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | source | api  |
      | zone   | DE-1 |

  Scenario: A parcel needs only a reference, a sender, a weight and a recipient
    Given a POST request to /api/parcels
    And a request payload using an application/json empty content template
    And the request payload properties are:
      | reference   | PX-REG-1002                                                       |
      | sender      | shop-example                                                      |
      | weightGrams | 450                                                               |
      | recipient   | {"name": "Mary Somerville", "postcode": "80331", "country": "DE"} |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | serviceLevel   | STANDARD        |
      | recipient.name | Mary Somerville |
    And the response payload property recipient.street is undefined

  Scenario: A parcel can be changed and cancelled before pickup
    Given a 1st ordered POST request to /api/parcels
    And a request payload using an application/json content example for 1st ordered request
    And the request payload property reference is 'PX-REG-1003' for 1st ordered request
    And a 2nd ordered PATCH request to /api/parcels/PX-REG-1003
    And a request payload using an application/json content example named 'Heavier' for 2nd ordered request
    And a 3rd ordered DELETE request to /api/parcels/PX-REG-1003
    And a 4th ordered GET request to /api/parcels/PX-REG-1003
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    And the 3rd ordered request is executed
    And the 4th ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 200
    And the response payload property weightGrams is '2500' for 2nd ordered response
    And the 3rd ordered response status code is 204
    And the 4th ordered response status code is 404

  Scenario: A reference can only be registered once
    Given a 1st ordered POST request to /api/parcels
    And a request payload using an application/json content example for 1st ordered request
    And the request payload property reference is 'PX-REG-1004' for 1st ordered request
    And a 2nd ordered POST request to /api/parcels
    And a request payload using an application/json content example named 'Express parcel' for 2nd ordered request
    And the request payload property reference is 'PX-REG-1004' for 2nd ordered request
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 1st ordered response status code is 201
    And the 2nd ordered response status code is 409
    And the response body contains 'already registered' for 2nd ordered response

  Scenario: Parcels over 30 kg are refused
    Given the OpenAPI validation levels are:
      | validation.request.body.schema.maximum | IGNORE |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference   | PX-REG-1005 |
      | weightGrams | 31000       |
    When the request is executed
    Then the response status code is 400
    And the response header Content-Type is 'application/problem+json'
    And the response payload property detail is 'weightGrams must be at most 30000'
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-REG-1005 |
    And the selection has 0 rows

  Scenario: The street can be left out
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-REG-1006'
    And the request payload property recipient.street is null
    When the request is executed
    Then the response status code is 201
    And the response payload property recipient.city is 'Berlin'
    And the response payload property recipient.street is undefined

  Scenario: A shop lists its own parcels
    Given a 1st ordered POST request to /api/parcels
    And a request payload using an application/json content example for 1st ordered request
    And the request payload properties for 1st ordered request are:
      | reference | PX-REG-1007   |
      | sender    | lark-ceramics |
    And a 2nd ordered GET request to /api/parcels?sender=lark-ceramics
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200
    And the response payload properties for 2nd ordered response are:
      | [0].reference | PX-REG-1007   |
      | [0].sender    | lark-ceramics |
    And the response payload property [1] is undefined for 2nd ordered response

  Scenario: Every parcel gets a signed shipping label
    Given a 1st ordered POST request to /api/parcels
    And a request payload using an application/json content example for 1st ordered request
    And the request payload property reference is 'PX-REG-1008' for 1st ordered request
    And a 2nd ordered GET request to /api/parcels/PX-REG-1008/label
    When the 1st ordered request is executed
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200
    And the response payload property reference is 'PX-REG-1008' for 2nd ordered response
    And the response payload properties for 2nd ordered response match:
      | barcode   | ^PX[0-9]{11}$  |
      | signature | ^[0-9a-f]{64}$ |
