Feature: Parcel assistant

  The parcel assistant asks a model two things: the address in the note a shop attached to a
  parcel, and the answer to a recipient's question, which the model gives once it has looked
  the parcel up. The service speaks OpenAI's API. The scenarios mock the model with the axx
  WireMock image, whose answers are the mapping files in ../infra/models/mappings, and check
  what the service does with each answer, and what it asked the model.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked models service with the following properties:
      | url | http://${sys:local.host}:8086 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The assistant reads the address in a shop's note
    Given a POST request to /api/assistant/address
    And a request payload using an application/json content example
    And the request payload property note is 'Please deliver to Mara Lindqvist, Birkenallee 3, 01067 Dresden. Ring twice.'
    When the request is executed
    Then the response status code is 200
    And the response payload properties are:
      | name     | Mara Lindqvist |
      | street   | Birkenallee 3  |
      | postcode | "01067"        |
      | city     | Dresden        |
      | country  | DE             |
    And the mocked models model was asked for the schemas/recipient-address.json schema in the request about 'Birkenallee 3'

  Scenario: A note the assistant cannot read an address in goes back to the shop
    Given a POST request to /api/assistant/address
    And a request payload using an application/json content example
    And the request payload property note is 'For Jonas Brandt, Kastanienweg 9, back entrance. Leave it with the neighbours.'
    When the request is executed
    Then the response status code is 422
    And the response payload property detail is 'the assistant cannot read an address in the note: ask the shop to check it'

  Scenario: The assistant looks the parcel up before it answers, and never tells the model the street
    Given a seeds/assistant-parcel.yaml db seed
    And a POST request to /api/assistant/questions
    And a request payload using an application/json content example
    And the request payload property question is 'Where is my parcel PX-AI-8102?'
    When the request is executed
    Then the response status code is 200
    And the response payload property answer is 'Your parcel PX-AI-8102 is out for delivery in Leipzig and arrives today.'
    And the mocked models model was offered the track_parcel tool in the request about 'PX-AI-8102'
    And the mocked models model was asked about 'PX-AI-8102' 2 times
    And the mocked models model's request about 'PX-AI-8102' contains 'OUT_FOR_DELIVERY'
    And the mocked models model's request about 'PX-AI-8102' does not contain 'Lindenweg 14'

  Scenario: A busy model is asked once more, then the recipient is asked to try again
    Given a POST request to /api/assistant/questions
    And a request payload using an application/json content example
    And the request payload property question is 'Has PX-AI-8104 left the depot yet?'
    When the request is executed
    Then the response status code is 503
    And the response header Retry-After is '30'
    And the mocked models model was asked about 'PX-AI-8104' 2 times
