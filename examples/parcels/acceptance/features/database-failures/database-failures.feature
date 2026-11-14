# These scenarios make the parcels table fail on purpose, so they run alone
# (run.exclusive in axx.yaml).
@isolated
Feature: Database failures

  The database can briefly refuse to store a parcel, for example when two transactions
  conflict. The service retries such failures, whether a shop registers the parcel or the
  service imports it from a manifest, and when they persist it tells the shop to try again
  later instead of failing with an internal error.

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
    And the parcels log with the following properties:
      | url | udp://0.0.0.0:5140 |

  Scenario: A brief database failure does not fail the registration
    Given a before insert trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
      | reference | PX-DBF-3001 |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-DBF-3001'
    When the request is executed
    Then the response status code is 201
    And the before insert trigger on the parcels.parcels table was raised 1 time
    And the parcels log has 1 entry matching 'msg="storing parcel failed, retrying" reference=PX-DBF-3001'
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-DBF-3001 |
    And the selection has 1 row

  Scenario: A brief database failure does not stop a manifest's import
    Given a before insert trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
      | reference | PX-DBF-3011 |
    And a seeds/manifest-retried.yaml db seed
    When within 10s the before insert trigger on the parcels.parcels table was raised 1 time
    Then within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-DBF-3011-1 |
      | status | IMPORTED      |
    And the parcels log has 1 entry matching 'msg="storing parcel failed, retrying" reference=PX-DBF-3011'

  Scenario: A lasting database failure asks the shop to try again later
    Given a before insert trigger on the parcels.parcels table will raise a 40001 exception where:
      | reference | PX-DBF-3002 |
    And a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload property reference is 'PX-DBF-3002'
    When the request is executed
    Then the response status code is 503
    And the response header Retry-After is '"5"'
    And the response payload property detail is 'the parcel could not be stored; try again later'
    And the before insert trigger on the parcels.parcels table was raised 3 times
    And the parcels log has 2 entries matching 'msg="storing parcel failed, retrying" reference=PX-DBF-3002'
    And the parcels log has an entry matching 'msg="registration refused; not announced" reference=PX-DBF-3002 reason="the database keeps failing"'

  Scenario: A brief database failure does not fail a change to a parcel
    Given a seeds/change-retried.yaml db seed
    And a before update trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
      | reference    | PX-DBF-3102 |
      | weight_grams | 2500        |
    And a PATCH request to /api/parcels/PX-DBF-3102
    And a request payload using an application/json content example
    When the request is executed
    Then the response status code is 200
    And the response payload property weightGrams is 2500
    And the before update trigger on the parcels.parcels table was raised 1 time
    And the parcels log has 1 entry matching 'msg="changing parcel failed, retrying" reference=PX-DBF-3102'

  Scenario: A lasting database failure keeps a parcel as it was when it is changed
    Given a seeds/change-refused.yaml db seed
    And a before update trigger on the parcels.parcels table will raise a 40001 exception where:
      | reference | PX-DBF-3101 |
    And a PATCH request to /api/parcels/PX-DBF-3101
    And a request payload using an application/json content example
    When the request is executed
    Then the response status code is 503
    And the response header Retry-After is '"5"'
    And the response payload property detail is 'parcel PX-DBF-3101 could not be changed; try again later'
    And the before update trigger on the parcels.parcels table was raised 3 times
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference    | PX-DBF-3101 |
      | weight_grams | 900         |
    And the selection has 1 row

  Scenario: A brief database failure does not fail a cancellation
    Given a seeds/cancel-retried.yaml db seed
    And a before delete trigger on the parcels.parcels table will raise a 40001 exception 1 time where:
      | reference | PX-DBF-3104 |
    And a DELETE request to /api/parcels/PX-DBF-3104
    When the request is executed
    Then the response status code is 204
    And the before delete trigger on the parcels.parcels table was raised 1 time
    And the parcels log has 1 entry matching 'msg="cancelling parcel failed, retrying" reference=PX-DBF-3104'
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-DBF-3104 |
    And the selection has 0 rows

  Scenario: A lasting database failure keeps a parcel when it is cancelled
    Given a seeds/cancel-refused.yaml db seed
    And a before delete trigger on the parcels.parcels table will raise a 40001 exception where:
      | reference | PX-DBF-3103 |
    And a DELETE request to /api/parcels/PX-DBF-3103
    When the request is executed
    Then the response status code is 503
    And the response header Retry-After is '"5"'
    And the before delete trigger on the parcels.parcels table was raised 3 times
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-DBF-3103 |
    And the selection has 1 row
