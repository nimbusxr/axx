Feature: Manifest import outcomes
  The importer registers valid manifest lines and rejects the ones we can't carry.

  Background:
    Given a parcels-db database with the following properties:
      | url      | jdbc:postgresql://postgres:5432/parcels |
      | user     | parcels                                 |
      | password | parcels                                 |
      | schema   | parcels                                 |

  Scenario: A valid manifest line is registered as a manifest parcel
    Given a seeds/manifest-outcome-valid.yaml database seed
    Then within 10s a selection of at least 1 row is retrieved from the parcels.parcels table where:
      | reference | PX-OUTCOME-VALID-01 |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | source     | manifest         |
      | manifestId | MF-OUTCOME-VALID |
      | lineId     | ML-OUTCOME-VALID |

  Scenario: An overweight manifest line is rejected
    Given a seeds/manifest-outcome-overweight.yaml db seed
    Then within 10s a 1st selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-OUTCOME-HEAVY       |
      | status | FAILED                 |
      | error  | weight exceeds 30000 g |
    And the 1st selection has 1 row
    And a 2nd selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-OUTCOME-HEAVY-01 |
    And the 2nd selection has 0 rows
