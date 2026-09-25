Feature: Carrier invoices

  Carriers upload their monthly invoices to the carrier-invoices bucket. Billing prices
  every line at the rate agreed with the carrier, for the weight the parcels platform
  measured, and disputes the lines billed above it and the parcels it never shipped.

  Background:
    Given the billing gcp project with the following properties:
      | project  | parcels-billing               |
      | endpoint | http://${sys:local.host}:4588 |
    And a seeds/carrier-rates.yaml bigquery seed

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
    And the INV-2026-09-KES2.csv object in the billing-disputes gcs bucket is identical to the expected/INV-2026-09-KES2-disputes.csv file
    And the INV-2026-09-KES2.json object in the billing-disputes gcs bucket has the following properties:
      | carrier        | KESTREL |
      | disputedLines  | 2       |
      | disputedAmount | 5.25    |
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

  Scenario: An invoice uploaded twice is reconciled once
    Given a seeds/shipments-her1.yaml firestore seed
    And the console log with the following properties:
      | url | file://.axx/logs/apps.log |
    When the invoices/INV-2026-09-HER1.csv file is uploaded to the carrier-invoices gcs bucket as heron/INV-2026-09-HER1.csv
    Then within 30s the invoices/INV-2026-09-HER1 firestore document has the following properties:
      | status | RECONCILED |
    When the invoices/INV-2026-09-HER1.csv file is uploaded to the carrier-invoices gcs bucket as heron/INV-2026-09-HER1.csv
    Then the console log has an entry matching 'invoice already reconciled.* invoice=INV-2026-09-HER1'
    And the billing.invoice_lines bigquery table has 1 row where:
      | invoice | INV-2026-09-HER1 |
