Feature: Label printing

  The depots print the shipping label of every registered parcel on printers that talk
  AMQP. The service sends each label to the printers of the parcel's service level, and
  the printers report every label they print. A report the service cannot use is set
  aside for the depot's staff, with the reason. Every message follows the service's
  AsyncAPI contract, save those of the older printers and the broken reports the
  scenarios send on purpose.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the depot amqp broker with the following properties:
      | url      | amqp://parcels:parcels@${sys:local.host}:5672/ |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml    |

  Scenario: A registered parcel's label goes to the printers of its service level
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-AMQ-8101'
    And the request payload property sender is 'alder-and-ash'
    And the request payload property serviceLevel is 'EXPRESS'
    When the request is executed
    Then the response status code is 201
    And within 10s the labels amqp exchange has a message where:
      | routing key   | print.express |
      | reference     | PX-AMQ-8101   |
      | serviceLevel  | EXPRESS       |
      | header sender | alder-and-ash |

  Scenario: A printer's report marks the parcel's label printed
    Given a seeds/printing-exchange.yaml db seed
    When a message is published to the printers amqp exchange with the routing key 'printed.LEJ':
      """
      {"reference": "PX-AMQ-8102", "printer": "LEJ-3", "printedAt": "2026-09-28T08:15:00Z"}
      """
    Then within 10s a selection of at least 1 row is retrieved from the parcels.parcels table where:
      | reference        | PX-AMQ-8102 |
      | label_printed_by | LEJ-3       |

  Scenario: An older printer reports straight to the service's queue
    Given a seeds/printing-queue.yaml db seed
    And the AsyncAPI validation levels are:
      | validation.message.payload.schema.required | WARN |
    When the amqp/label-printed-PX-AMQ-8103.json message is sent to the parcels.label-printed amqp queue with the following properties:
      | header printer | DRS-1 |
    Then within 10s a selection of at least 1 row is retrieved from the parcels.parcels table where:
      | reference        | PX-AMQ-8103 |
      | label_printed_by | DRS-1       |

  Scenario: A report of a parcel the service does not know is set aside
    When the amqp/label-printed-unknown.json message is published to the printers amqp exchange with the following properties:
      | routing key    | printed.LEJ |
      | header printer | LEJ-3       |
    Then within 10s the parcels.label-printed.rejected amqp queue has a message where:
      | reference      | PX-AMQ-8199    |
      | header reason  | unknown parcel |
      | header printer | LEJ-3          |

  Scenario: A report without a parcel reference is set aside
    Given the AsyncAPI validation levels are:
      | validation.message.payload | IGNORE |
    When a message is sent to the parcels.label-printed amqp queue:
      """
      {"printer": "LEJ-3", "job": "JOB-AMQ-8105"}
      """
    Then within 10s the parcels.label-printed.rejected amqp queue has a message where:
      | job           | JOB-AMQ-8105       |
      | header reason | not a label report |
