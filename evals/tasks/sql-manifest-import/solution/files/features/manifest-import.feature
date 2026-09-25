Feature: Manifest import
  Shops write manifest lines into the manifest table; the service imports each line as a
  registered parcel, or rejects it with a reason.

  Background:
    Given a parcels-db database with the following properties:
      | url      | jdbc:postgresql://postgres:5432/parcels |
      | user     | parcels                                 |
      | password | parcels                                 |
      | schema   | parcels                                 |

  Scenario: A valid manifest line becomes a registered parcel
    Given a seeds/manifest-import-valid.yaml db seed
    Then within 10s a selection of at least 1 row is retrieved from the parcels.parcels table where:
      | reference     | PX-IMPORT-VALID-01 |
      | sender        | shop-import-valid  |
      | weight_grams  | 1800               |
      | service_level | EXPRESS            |
      | status        | REGISTERED         |
    And the selection has 1 row
    And the 1st row recipient property for the selection json properties are:
      | name     | Mia Import     |
      | street   | Hafenstrasse 1 |
      | city     | Hamburg        |
      | postcode | 20457          |
      | country  | DE             |
    And the 1st row details property for the selection json properties are:
      | source     | manifest            |
      | manifestId | MF-IMPORT-VALID     |
      | lineId     | ML-IMPORT-VALID-01  |

  Scenario: An imported manifest line is marked IMPORTED
    Given a seeds/manifest-import-marked.yaml db seed
    Then within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id               | ML-IMPORT-MARKED-01 |
      | status           | IMPORTED            |
      | parcel_reference | PX-IMPORT-MARKED-01 |
    And the selection has 1 row

  Scenario: An overweight manifest line is rejected and creates no parcel
    Given a seeds/manifest-import-overweight.yaml db seed
    Then within 10s a 1st selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-IMPORT-HEAVY-01     |
      | status | REJECTED               |
      | error  | weight exceeds 30000 g |
    And the 1st selection has 1 row
    And a 2nd selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-IMPORT-HEAVY-01 |
    And the 2nd selection has 0 rows
