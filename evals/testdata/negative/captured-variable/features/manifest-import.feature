Feature: Manifest import
  A programming answer: it passes a value from one step to the next.

  Background:
    Given a parcels-db database with the following properties:
      | url      | jdbc:postgresql://postgres:5432/parcels |
      | user     | parcels                                 |
      | password | parcels                                 |
      | schema   | parcels                                 |

  Scenario: A valid manifest line becomes a registered parcel
    Given a seeds/negative-import.yaml db seed
    Then within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-NEG-VAR-01 |
      | status | IMPORTED      |
    And the selection has 1 row
    And a 2nd selection of rows is retrieved from the parcels.parcels table where:
      | reference | ${var:parcelReference} |
    And the 2nd selection has 1 row
