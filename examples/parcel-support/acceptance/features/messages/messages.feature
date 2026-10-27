Feature: Customers' messages

  A customer's message (from the contact form, or an e-mail) opens a ticket. The help
  centre's search finds the article that answers it (its embeddings find the articles near
  the message, and a reranker reads the message with each), and a language model drafts a
  reply from the article, for an agent to send. A message no article answers is for an agent
  to answer.

  Background:
    Given the support service with the following properties:
      | url     | http://${sys:local.host}:8800              |
      | openapi | http://${sys:local.host}:8800/openapi.json |

  Scenario: A question the help centre answers gets its article and a draft reply
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | anna.weber@example.com                                                           |
      | text     | Hello, can I still change the delivery address of PX-5102? I'm moving on Friday. |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | channel | message                                                                                                                                                                                                       |
      | parcel  | PX-5102                                                                                                                                                                                                       |
      | article | change-delivery-address                                                                                                                                                                                       |
      | status  | DRAFTED                                                                                                                                                                                                       |
      | draft   | Hello, yes: you can change the delivery address of PX-5102 until it is out for delivery. Open its tracking page, choose Change delivery and enter your new address.\n\nKind regards, the Parcels support team |

  Scenario: A question no article answers is for an agent
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | tom.becker@example.com                          |
      | text     | Do you collect parcels from offices on Sundays? |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | channel | message |
      | status  | NEW     |
    And the response payload property article is null
    And the response payload property draft is null

  Scenario: A question near an article that does not answer it is for an agent
    # Its words are those of the customs charges' article, but it is a business's question.
    Given a POST request to /api/messages
    And a request payload using an application/json content example
    And the request payload properties are:
      | customer | orders@harbour-tea.example                                                       |
      | text     | Do customs charges apply to a business account that sends parcels abroad weekly? |
    When the request is executed
    Then the response status code is 201
    And the response payload properties are:
      | channel | message |
      | status  | NEW     |
    And the response payload property article is null
