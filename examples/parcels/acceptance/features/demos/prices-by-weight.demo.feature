# demo: [card 3s] What a parcel costs, by its weight
Feature: Prices by weight, as a demo

  A demo of a Scenario Outline: `axx demo` makes one film of it, with a chapter for each
  row. A cue's <grams> is the row's value, as a step's is.

  Scenario Outline: A shop asks what a parcel of <grams> grams costs
    # demo: [cut]
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a POST request to /api/quotes
    And a request payload using an application/json content example named 'Domestic parcel'
    And the request payload property weightGrams is '<grams>'

    # demo: [action] A shop asks what a parcel of <grams> grams costs within Germany.
    When the request is executed

    # demo: [pause 1s] We answer with its price, in cents: <price>.
    Then the response payload property priceCents is '<price>'

    Examples:
      | grams | price |
      | 500   | 490   |
      | 7500  | 890   |
      | 25000 | 1290  |
