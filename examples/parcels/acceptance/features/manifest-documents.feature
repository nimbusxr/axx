Feature: Manifest documents

  Once the service has imported or rejected every line of a manifest, it writes the
  manifest's documents to its export folder, where the shop's system collects them: the
  import report (a CSV file for the shop's system, a workbook for its staff), a summary,
  and the handover note the driver signs when collecting the parcels.

  Background:
    Given a parcels-db database with the following properties:
      | url      | postgres://localhost:5432/parcels |
      | user     | parcels                           |
      | password | parcels                           |
    And the exports folder with the following properties:
      | path | ../infra/exports |

  Scenario: The import report says what became of every line
    Given a seeds/manifest-heron.yaml db seed
    Then within 10s the exports folder has a file named manifests/M-HERON-0501/report.csv
    And the manifests/M-HERON-0501/report.csv file in the exports folder has a row where:
      | line   | ML-HER-0501-3         |
      | status | REJECTED              |
      | reason | unknown service level |
    And the manifests/M-HERON-0501/report.csv file in the exports folder is identical to the reports/M-HERON-0501.csv file
    And the manifests/M-HERON-0501/summary.json file in the exports folder has the following properties:
      | manifest | M-HERON-0501 |
      | shop     | heron-prints |
      | lines    | 3            |
      | imported | 2            |
      | rejected | 1            |

  Scenario: The shop's staff read the report in a workbook
    Given a seeds/manifest-osprey.yaml db seed
    Then within 10s the manifests/M-OSPREY-0502/report.xlsx file in the exports folder has a row where:
      | line      | ML-OSP-0502-2 |
      | reference | PX-OSP-5102   |
      | status    | IMPORTED      |

  Scenario: The handover note lists the parcels the driver collects
    Given a seeds/manifest-wren.yaml db seed
    Then within 10s the manifests/M-WREN-0503/handover.pdf file in the exports folder contains "Parcels to collect: 2"
    And the manifests/M-WREN-0503/handover.pdf file in the exports folder contains "PX-WRN-5201 Marie Curie, 01067 Dresden 1200 g STANDARD"
