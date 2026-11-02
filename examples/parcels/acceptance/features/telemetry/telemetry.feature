Feature: What the service reports

  The service reports what it does through OpenTelemetry, to a Collector that runs with or
  without these tests. Each registration is traced from the request to the database, logged
  in its trace and counted by its shop, and what it reports says nothing of who a parcel is
  going to.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And the parcels-api telemetry with the following properties:
      | url     | file://.axx/otel/telemetry.jsonl |
      | service | parcels-api                      |

  Scenario: A registration is traced from the request to the database
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-OTL-9801'
    When the request is executed
    Then the response status code is 201
    And within 10s the trace has a span where:
      | name                                | POST /api/parcels |
      | kind                                | SERVER            |
      | service                             | parcels-api       |
      | attribute http.route                | /api/parcels      |
      | attribute http.response.status_code | 201               |
    And the "check address" span is a child of the "POST /api/parcels" span
    And the "store parcel" span is a child of the "POST /api/parcels" span
    And the "store parcel" span took less than 2s
    And the trace has no span where:
      | status | ERROR |

  Scenario: A registered parcel's log is in its trace
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-OTL-9802'
    When the request is executed
    Then the response status code is 201
    And within 10s the trace has a log where:
      | severity            | INFO              |
      | body                | parcel registered |
      | attribute reference | PX-OTL-9802       |

  Scenario: A registered parcel is counted, by its shop
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-OTL-9803'
    And the request payload property sender is 'otel-metrics-shop'
    And the request payload property weightGrams is 1200
    When the request is executed
    Then the response status code is 201
    And within 30s the parcels-api telemetry counted 1 parcels_registered where:
      | attribute sender | otel-metrics-shop |
    And within 30s the parcels-api telemetry recorded 1 parcel_weight values where:
      | attribute sender | otel-metrics-shop |
    And within 30s the parcels-api telemetry's last_parcel_weight gauge is 1200 where:
      | attribute sender | otel-metrics-shop |

  Scenario: A refused registration is not counted
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-OTL-9804       |
      | sender             | otel-refused-shop |
      | recipient.postcode | "99990"           |
    When the request is executed
    Then the response status code is 422
    And within 10s the trace has a span where:
      | name   | check address |
      | status | ERROR         |
    And the parcels-api telemetry counted 0 parcels_registered where:
      | attribute sender | otel-refused-shop |

  Scenario: A recipient's name stays out of what the service reports
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-OTL-9805'
    And the request payload property recipient.name is 'Marta Keller'
    When the request is executed
    Then the response status code is 201
    And within 10s the trace has a log where:
      | attribute reference | PX-OTL-9805 |
    And the telemetry does not contain 'Marta Keller'
