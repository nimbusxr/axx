Feature: Manifest import

  Shops with many parcels upload a manifest instead of calling the API: their systems
  add one line per parcel to the manifest table, and the service imports every line
  with the same rules as the API, marking it IMPORTED or REJECTED with the reason.

  Background:
    Given a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the mocked addresses service with the following properties:
      | url | http://${sys:local.host}:8081 |
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |

  Scenario: Manifest lines become parcels
    Given a seeds/manifest-kestrel.yaml db seed
    When within 10s a selection of at least 2 rows is retrieved from the parcels.manifest_lines table where:
      | manifest_id | M-KESTREL-0412 |
      | status      | IMPORTED       |
    Then the selection has 2 rows
    And a 2nd selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
      | manifestId | M-KESTREL-0412 |
    And the 2nd selection has 2 rows
    And the 1st row details property for the 2nd selection json properties are:
      | source | manifest |

  Scenario: Invalid manifest lines are rejected with the reason
    Given a seeds/manifests/xml/rejected-lines.xml db seed
    When within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-FJORD-0101-1        |
      | status | REJECTED               |
      | error  | weight exceeds 30000 g |
    And within 10s a 2nd selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-FJORD-0101-2       |
      | status | REJECTED              |
      | error  | unknown service level |
    Then the selection has 1 row
    And the 2nd selection has 1 row
    And a 3rd selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
      | manifestId | M-FJORD-0101 |
    And the 3rd selection has 0 rows
    And the console log has entries matching:
      | msg="manifest line processed" line=ML-FJORD-0101-1 status=REJECTED reason="weight exceeds 30000 g" |
      | msg="manifest line processed" line=ML-FJORD-0101-2 status=REJECTED reason="unknown service level"  |

  Scenario: A reference that is already registered is not imported again
    Given a seeds/manifest-duplicate.yaml db seed
    When within 10s a selection of at least 1 row is retrieved from the parcels.manifest_lines table where:
      | id     | ML-KES-0413-1       |
      | status | REJECTED            |
      | error  | duplicate reference |
    Then the selection has 1 row

  Scenario: A large manifest is imported completely
    Given a seeds/manifests/csv/bulk-manifest/parcels.manifest_lines.csv db seed
    When within 20s a selection of at least 5 rows is retrieved from the parcels.manifest_lines table where:
      | manifest_id | M-MAPLE-0019 |
      | status      | IMPORTED     |
    Then the selection has 5 rows
