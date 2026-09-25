Feature: Customs declarations

  Brokers upload a parcel's commercial invoice and file its declaration on the
  customs-filings queue. Parcels worth up to 150 EUR are cleared at once, with a
  certificate; others owe 20% duties first. A declaration without its invoice is held.

  Background:
    Given the customs azure storage account with the following properties:
      | connection string | DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=bG9jYWw=;BlobEndpoint=http://${sys:local.host}:4577/devstoreaccount1; |
    And the customs service bus namespace with the following properties:
      | connection string   | Endpoint=sb://${sys:local.host}:5673;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=local;UseDevelopmentEmulator=true; |
      | management endpoint | http://${sys:local.host}:4577/devstoreaccount1-servicebus |

  Scenario: A low-value parcel is cleared
    When the invoices/DEC-7101.json file is uploaded to the commercial-invoices blob container
    And the messages/filing-DEC-7101.json message is sent to the customs-filings service bus queue with the following properties:
      | broker | ACME-CUSTOMS |
    Then within 30s the clearances blob container has a blob named DEC-7101.json
    And the DEC-7101.json blob in the clearances blob container has the following properties:
      | declaration | DEC-7101 |
      | status      | CLEARED  |
      | duties      | 0        |
      | value       | 45       |
    And the DEC-7101/invoice.json blob in the customs-archive blob container is identical to the invoices/DEC-7101.json file
    And the customs-events service bus topic has a message where:
      | property eventType | DeclarationCleared |
      | declaration        | DEC-7101           |

  Scenario: A parcel above the de minimis value owes duties
    When the invoices/DEC-7102.json file is uploaded to the commercial-invoices blob container
    And the messages/filing-DEC-7102.json message is sent to the customs-filings service bus queue with the following properties:
      | broker | NORTHSTAR-BROKERS |
    Then within 30s the duty-payments service bus queue has a message where:
      | declaration     | DEC-7102          |
      | amount          | 96                |
      | currency        | EUR               |
      | property broker | NORTHSTAR-BROKERS |
    And the customs-events service bus topic has a message where:
      | property eventType | DeclarationHeld |
      | declaration        | DEC-7102        |
      | reason             | DUTIES_DUE      |

  Scenario: A declaration without its invoice is held
    When a message is sent to the customs-filings service bus queue:
      """
      {"declaration": "DEC-7103", "parcel": "PX-7103", "invoice": "DEC-7103.json"}
      """
    Then within 30s the customs-events service bus topic has a message where:
      | property eventType | DeclarationHeld |
      | declaration        | DEC-7103        |
      | reason             | MISSING_INVOICE |
