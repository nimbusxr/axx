Feature: Shop emails

  The service emails each shop at the contact address of its settings: the label of every
  parcel it registers, for its label printer, and each delivery, when the shop asked to be
  told. It sends its mail by SMTP to its mail server: axx's Mailpit, in the tests. An email
  the mail server refuses never stops what the shop asked for.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the shops mailbox with the following properties:
      | url | http://${sys:local.host}:8025 |
    And the parcels log with the following properties:
      | url | udp://0.0.0.0:5140 |

  Scenario: A shop gets its parcel's label by email
    Given a seeds/mail-registered.yaml db seed
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-MAIL-9701'
    And the request payload property sender is 'juniper-and-jay'
    When the request is executed
    Then the response status code is 201
    And within 10s the shops mailbox has an email where:
      | to         | orders@juniper-and-jay.example   |
      | from       | no-reply@parcels.example         |
      | subject    | Parcel PX-MAIL-9701 registered   |
      | text       | Its label is attached            |
      | attachment | PX-MAIL-9701-label.zpl           |

  Scenario: A shop that asked to be told of deliveries gets an email
    Given a seeds/mail-delivered.yaml db seed
    And a POST request to /api/courier/callbacks
    And a request payload using an application/json content example named 'Delivered'
    And the request payload property reference is 'PX-MAIL-9702'
    And the request is signed in the X-Courier-Signature header with the following properties:
      | key   | ${sys:courier.callback-key} |
      | value | sha256={signature}          |
    When the request is executed
    Then the response status code is 204
    And within 10s the shops mailbox has an email where:
      | to      | orders@larkspur-lane.example                         |
      | subject | Parcel PX-MAIL-9702 delivered                        |
      | html    | <b>PX-MAIL-9702</b> was delivered in Leipzig         |

  Scenario: A registration email the mail server refuses does not stop the registration
    Given a seeds/mail-refused.yaml db seed
    And the shops mailbox refuses mail to '*@quince-and-quill.example' with code 451
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-MAIL-9703'
    And the request payload property sender is 'quince-and-quill'
    When the request is executed
    Then the response status code is 201
    And within 10s the parcels log has an entry matching 'msg="emailing the shop failed" reference=PX-MAIL-9703 shop=quince-and-quill err="451 '
