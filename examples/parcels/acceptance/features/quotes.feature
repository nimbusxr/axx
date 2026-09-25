Feature: Price quotes

  Shops ask for a price before they register a parcel. The price depends on the service
  level, the weight, whether the parcel goes abroad, and the delivery zone the address
  service assigns to the recipient's postcode.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |

  Scenario: A standard parcel within Germany
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | zone         | DE-1 |
      | priceCents   | 690  |
      | currency     | EUR  |
      | deliveryDays | 2    |

  Scenario: Express delivery abroad
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Express abroad'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | zone         | FR-1 |
      | priceCents   | 1790 |
      | deliveryDays | 2    |

  Scenario Outline: Heavier parcels cost more
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property weightGrams is '<grams>'
    When the request is executed
    Then the response status code is 200
    And the response payload property priceCents is '<price>'

    Examples:
      | grams | price |
      | 500   | 490   |
      | 4000  | 690   |
      | 7500  | 890   |
      | 25000 | 1290  |

  Scenario: Remote areas cost more and take a day longer
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property recipient.postcode is '"27498"'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | zone         | DE-REMOTE |
      | priceCents   | 990       |
      | deliveryDays | 3         |

  Scenario: The delivery zone comes from the address service
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property recipient.postcode is '"50667"'
    When the request is executed
    Then the response status code is 200
    And the mocked GET request to /v1/postcodes/DE/50667 named zone-lookup was received by addresses
    And the header X-Api-Key for mocked request named zone-lookup is 'example-address-key'

  # The address service answers with a field its OpenAPI document does not list yet. That
  # breaks its documented contract, but the quote must not depend on it.
  Scenario: Quotes keep working when the address service sends a field it has not documented
    Given the OpenAPI validation levels for the mocked addresses service are:
      | validation.response.body.schema.additionalProperties | WARN |
    And a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property recipient.postcode is '"80995"'
    When the request is executed
    Then the response status code is 200
    And the response payload property zone is 'DE-8'
    And the mocked GET request to /v1/postcodes/DE/80995 named zone-lookup was received by addresses

  Scenario: No quote for an undeliverable postcode
    Given a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property recipient.postcode is '"99999"'
    When the request is executed
    Then the response status code is 422
    And the response header Content-Type is 'application/problem+json'
    And the response payload property detail is 'address not deliverable: no delivery to this postcode'
