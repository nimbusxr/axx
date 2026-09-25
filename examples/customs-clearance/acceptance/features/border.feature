Feature: Parcels at the border

  Carriers announce parcels reaching the border on the border-events topic. A cleared
  parcel is released for delivery; one that is not is held.

  Background:
    Given the customs azure storage account with the following properties:
      | connection string | DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=bG9jYWw=;BlobEndpoint=http://${sys:local.host}:4577/devstoreaccount1; |
    And the customs service bus namespace with the following properties:
      | connection string   | Endpoint=sb://${sys:local.host}:5673;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=local;UseDevelopmentEmulator=true; |
      | management endpoint | http://${sys:local.host}:4577/devstoreaccount1-servicebus |

  Scenario: A cleared parcel is released when it reaches the border
    Given the invoices/DEC-7104.json file is uploaded to the commercial-invoices blob container
    And the messages/filing-DEC-7104.json message is sent to the customs-filings service bus queue
    And within 30s the clearances blob container has a blob named DEC-7104.json
    When a message is sent to the border-events service bus topic:
      """
      {"declaration": "DEC-7104", "parcel": "PX-7104"}
      """
    Then within 30s the customs-events service bus topic has a message where:
      | property eventType | ReleasedForDelivery |
      | declaration        | DEC-7104            |

  Scenario: A parcel that is not cleared is held at the border
    When a message is sent to the border-events service bus topic:
      """
      {"declaration": "DEC-7105", "parcel": "PX-7105"}
      """
    Then within 30s the customs-events service bus topic has a message where:
      | property eventType | HeldAtBorder |
      | declaration        | DEC-7105     |
