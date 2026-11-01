Feature: Carrier invoices

  Carriers upload their monthly invoices to the carrier-invoices bucket. Billing prices
  every line at the rate agreed with the carrier, for the weight the parcels platform
  measured, and disputes the lines billed above it and the parcels it never shipped, in
  a letter to the carrier. A carrier that repacks a parcel whose packaging gave way
  uploads a photo of the repacked parcel with its invoice: billing pays the repack when
  the photo shows a parcel.

  Background:
    Given the billing gcp project with the following properties:
      | project  | parcels-billing               |
      | endpoint | http://${sys:local.host}:4588 |
    And a seeds/carrier-rates.yaml bigquery seed

  # Known flake: floci's BigQuery emulator once lost this invoice's lines when three
  # invoices' inserts arrived within a millisecond. Not reproducible; rerun if it recurs.
  Scenario: An invoice at the agreed rates is reconciled
    Given a seeds/shipments-kes1.yaml firestore seed
    When the invoices/INV-2026-09-KES1.csv file is uploaded to the carrier-invoices gcs bucket as kestrel/INV-2026-09-KES1.csv
    Then within 30s the invoices/INV-2026-09-KES1 firestore document has the following properties:
      | status          | RECONCILED |
      | lines           | 2          |
      | totals.billed   | 6.58       |
      | totals.disputed | 0          |
    And the billing.invoice_lines bigquery table has 2 rows where:
      | invoice | INV-2026-09-KES1 |
      | status  | MATCHED          |
    And the invoice-events pubsub topic has a message where:
      | attribute eventType | InvoiceReconciled |
      | invoice             | INV-2026-09-KES1  |
      | status              | RECONCILED        |

  Scenario: Overcharged lines and unknown parcels are disputed
    Given a seeds/shipments-kes2.yaml firestore seed
    When the invoices/INV-2026-09-KES2.csv file is uploaded to the carrier-invoices gcs bucket as kestrel/INV-2026-09-KES2.csv
    Then within 30s the billing-disputes gcs bucket has an object named INV-2026-09-KES2.csv
    And the INV-2026-09-KES2.csv object in the billing-disputes gcs bucket has a row where:
      | parcel | PX-5199          |
      | status | UNKNOWN_SHIPMENT |
      | billed | "4.10"           |
    And the INV-2026-09-KES2.csv object in the billing-disputes gcs bucket is identical to the expected/INV-2026-09-KES2-disputes.csv file
    And the INV-2026-09-KES2.json object in the billing-disputes gcs bucket has the following properties:
      | carrier        | KESTREL |
      | disputedLines  | 2       |
      | disputedAmount | 5.25    |
    And the INV-2026-09-KES2-letter.txt object in the billing-disputes gcs bucket contains "we dispute 2 of its lines, 5.25 in all"
    And the INV-2026-09-KES2-letter.txt object in the billing-disputes gcs bucket contains "PX-5199 billed 4.10, a parcel we did not ship with you"
    And the invoices/INV-2026-09-KES2 firestore document has the following properties:
      | status          | DISPUTED |
      | totals.billed   | 9.3      |
      | totals.disputed | 5.25     |
    And the billing.invoice_lines bigquery table has a row where:
      | invoice  | INV-2026-09-KES2 |
      | parcel   | PX-5103          |
      | status   | OVERCHARGED      |
      | billed   | 2.5              |
      | expected | 1.35             |
    And the invoice-events pubsub topic has a message where:
      | attribute eventType | InvoiceDisputed  |
      | invoice             | INV-2026-09-KES2 |
      | disputed            | 5.25             |

  Scenario: A repack is paid only with a photo of the repacked parcel
    Given a seeds/shipments-kes3.yaml firestore seed
    And the photos/repacked-parcel.png file is uploaded to the carrier-invoices gcs bucket as kestrel/INV-2026-09-KES3/PX-5105.png
    And the photos/delivery-note.png file is uploaded to the carrier-invoices gcs bucket as kestrel/INV-2026-09-KES3/PX-5106.png
    When the invoices/INV-2026-09-KES3.csv file is uploaded to the carrier-invoices gcs bucket as kestrel/INV-2026-09-KES3.csv
    Then within 30s the billing-disputes gcs bucket has an object named INV-2026-09-KES3.csv
    And the INV-2026-09-KES3.csv object in the billing-disputes gcs bucket has a row where:
      | parcel | PX-5106     |
      | status | NO_EVIDENCE |
      | billed | "0.75"      |
    And the INV-2026-09-KES3-letter.txt object in the billing-disputes gcs bucket contains "PX-5106  billed 0.75 for a repack, without a photo of the repacked parcel"
    And the billing.invoice_lines bigquery table has a row where:
      | invoice | INV-2026-09-KES3 |
      | parcel  | PX-5105          |
      | status  | MATCHED          |

  Scenario: An invoice uploaded twice is reconciled once
    Given a seeds/shipments-her1.yaml firestore seed
    And the console log with the following properties:
      | url | file://.axx/logs/services.log |
    And the invoices/INV-2026-09-HER1.csv file is uploaded to the carrier-invoices gcs bucket as heron/INV-2026-09-HER1.csv
    And within 30s the invoices/INV-2026-09-HER1 firestore document has the following properties:
      | status | RECONCILED |
    When the invoices/INV-2026-09-HER1.csv file is uploaded to the carrier-invoices gcs bucket as heron/INV-2026-09-HER1.csv
    Then the console log has an entry matching 'invoice already reconciled.* invoice=INV-2026-09-HER1'
    And the billing.invoice_lines bigquery table has 1 row where:
      | invoice | INV-2026-09-HER1 |
