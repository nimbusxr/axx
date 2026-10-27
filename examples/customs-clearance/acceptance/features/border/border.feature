Feature: Parcels at the border

  Carriers announce parcels reaching the border on the border-events topic. A cleared
  parcel is released for delivery; one that is not is held. An officer may open a parcel
  and photograph its contents: a cleared parcel whose contents differ from what its
  invoice declares is held.

  Background:
    Given the customs azure storage account with the following properties:
      | connection string | DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=bG9jYWw=;BlobEndpoint=http://${sys:local.host}:4577/devstoreaccount1; |
    And the customs service bus namespace with the following properties:
      | connection string   | Endpoint=sb://${sys:local.host}:5672;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true; |
      | management endpoint | http://${sys:local.host}:5300                                                                                                                 |

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

  Scenario: An inspected parcel whose contents match its invoice is released
    Given the invoices/DEC-7106.json file is uploaded to the commercial-invoices blob container
    And the messages/filing-DEC-7106.json message is sent to the customs-filings service bus queue
    And within 30s the clearances blob container has a blob named DEC-7106.json
    And the inspections/teddy-bear.png file is uploaded to the inspections blob container as DEC-7106.png
    When a message is sent to the border-events service bus topic:
      """
      {"declaration": "DEC-7106", "parcel": "PX-7106", "inspection": "DEC-7106.png"}
      """
    Then within 30s the customs-events service bus topic has a message where:
      | property eventType | ReleasedForDelivery |
      | declaration        | DEC-7106            |

  Scenario: An inspected parcel whose contents differ from its invoice is held
    Given the invoices/DEC-7107.json file is uploaded to the commercial-invoices blob container
    And the messages/filing-DEC-7107.json message is sent to the customs-filings service bus queue
    And within 30s the clearances blob container has a blob named DEC-7107.json
    And the inspections/phone.png file is uploaded to the inspections blob container as DEC-7107.png
    When a message is sent to the border-events service bus topic:
      """
      {"declaration": "DEC-7107", "parcel": "PX-7107", "inspection": "DEC-7107.png"}
      """
    Then within 30s the customs-events service bus topic has a message where:
      | property eventType | HeldAtBorder                                          |
      | declaration        | DEC-7107                                              |
      | reason             | CONTENTS_DIFFER: the photo shows a cellular telephone |
