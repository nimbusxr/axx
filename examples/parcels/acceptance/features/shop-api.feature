Feature: The shop API

  The shops' own systems read their parcels with a token: one the shops' platform signs
  with the key it shares with the service, or one a shop's system gets from the service
  with its client credentials. A token is for one shop, and for no other.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The shops' platform lists a shop's parcels with a token it signed
    Given a seeds/shop-api.yaml db seed
    And the shop token with the following properties:
      | key        | ${sys:shops.token-key} |
      | claim.shop | hawthorn-home          |
    And a GET request to /api/shops/hawthorn-home/parcels
    And the request is authorized with the shop token
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | shop                 | hawthorn-home |
      | parcels[0].reference | PX-API-9201   |
      | parcels[1].reference | PX-API-9202   |

  Scenario: A token for another shop is refused
    Given the shop token with the following properties:
      | key        | ${sys:shops.token-key} |
      | claim.shop | maple-crafts           |
    And a GET request to /api/shops/hawthorn-home/parcels
    And the request is authorized with the shop token
    When the request is executed
    Then the response status code is 403
    And the response payload property detail is 'the token is not for hawthorn-home'

  Scenario: A shop's system gets a token with its client credentials
    Given a seeds/shop-api-credentials.yaml db seed
    And the shop-system token with the following properties:
      | token url     | http://localhost:8400/oauth/token |
      | client id     | wisteria-way                      |
      | client secret | ${sys:shops.wisteria-secret}      |
    And a GET request to /api/shops/wisteria-way/parcels
    And the request is authorized with the shop-system token
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | shop                 | wisteria-way |
      | parcels[0].reference | PX-API-9203  |
