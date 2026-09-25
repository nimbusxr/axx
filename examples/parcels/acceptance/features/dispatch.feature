Feature: Changes during dispatch

  While the depot dispatches a parcel it holds the parcel's record. The parcel cannot be
  changed or cancelled until the depot lets go of it, and the shop is told to try again.

  Background:
    Given the parcels service with the following properties:
      | url     | http://localhost:8400              |
      | openapi | http://localhost:8400/openapi.json |
    And a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |

  Scenario: A parcel can be changed again once the depot lets go of it
    Given a seeds/dispatching.yaml db seed
    And the rows in the parcels.parcels table are locked where:
      | reference | PX-DSP-2001 |
    And a 1st ordered PATCH request to /api/parcels/PX-DSP-2001
    And a request payload using an application/json content example named 'Heavier' for 1st ordered request
    And a 2nd ordered PATCH request to /api/parcels/PX-DSP-2001
    And a request payload using an application/json content example named 'Heavier' for 2nd ordered request
    When the 1st ordered request is executed
    Then the 1st ordered response status code is 409
    And the response payload property detail is 'parcel PX-DSP-2001 is being dispatched and cannot be changed now' for 1st ordered response
    When the row locks are released
    And the 2nd ordered request is executed
    Then the 2nd ordered response status code is 200
    And the response payload property weightGrams is '2500' for 2nd ordered response
