Feature: Courier callbacks

  The courier calls the service back as it collects and delivers parcels, and signs each
  callback with the key they share. The service records a signed callback and refuses one
  it cannot trust. When a parcel is delivered, the service tells its shop in a webhook it
  signs, before it answers the courier.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |
    And the mocked shops service with the following properties:
      | url | http://${sys:local.host}:8083 |

  Scenario: The courier's signed callback records the collection
    Given a seeds/callback-collected.yaml db seed
    And a POST request to /api/courier/callbacks
    And a request payload using an application/json content example named 'Collected'
    And the request payload property reference is 'PX-WHK-9101'
    And the request is signed in the X-Courier-Signature header with the following properties:
      | key   | ${sys:courier.callback-key} |
      | value | sha256={signature}          |
    When the request is executed
    Then the response status code is 204
    And within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id    | PX-WHK-9101 |
      | status | COLLECTED   |

  Scenario: A callback signed with another key is refused
    Given a seeds/callback-refused.yaml db seed
    And a POST request to /api/courier/callbacks
    And a request payload using an application/json content example named 'Collected'
    And the request payload property reference is 'PX-WHK-9102'
    And the request is signed in the X-Courier-Signature header with the following properties:
      | key   | not-the-courier-key |
      | value | sha256={signature}  |
    When the request is executed
    Then the response status code is 401
    And the response payload property detail is "the X-Courier-Signature is not the courier's signature of the body"

  Scenario: A shop hears of its parcel's delivery in a signed webhook
    Given a seeds/callback-delivered.yaml db seed
    And a POST request to /api/courier/callbacks
    And a request payload using an application/json content example named 'Delivered'
    And the request payload property reference is 'PX-WHK-9103'
    And the request is signed in the X-Courier-Signature header with the following properties:
      | key   | ${sys:courier.callback-key} |
      | value | sha256={signature}          |
    When the request is executed
    Then the response status code is 204
    And the mocked POST request to path /webhooks/linden-and-lace named delivered was received by shops
    And the payload properties for mocked request named delivered on shops are:
      | type      | parcel.delivered |
      | reference | PX-WHK-9103      |
    And the mocked request named delivered on shops is signed as a standard webhook with the key '${sys:shops.webhook-key}'
