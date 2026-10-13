@mobile @ios
Feature: A courier attaches a document to a delivery, on iOS

  A recipient who will not be at home can leave the courier their authorization to put the
  parcel at a safe place: a signed page they send the courier, which the courier keeps in the
  phone's files. At the door, the courier attaches it to the delivery from the couriers' iPhone
  app (../courier/ios), picked in the document picker, and the service keeps it with the
  parcel. The phone has the recipient's authorization On My iPhone before the app starts.

  Background:
    Given the courier ios app with the following properties:
      | app       | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device    | ${sys:ios.device}                                      |
      | timezone  | Europe/Berlin                                          |
      | documents | documents/safe-place-PX-IOS-9591.pdf                   |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The recipient's safe-place authorization is kept with the parcel
    Given a seeds/courier-app-ios-documents.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-38"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9591" link
    And the courier app shows "Riebeckstr. 22, 04317 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "safe-place-PX-IOS-9591" list item is tapped in the courier app
    Then the courier app shows "safe-place-PX-IOS-9591.pdf attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9591                |
      | name         | safe-place-PX-IOS-9591.pdf |
      | content_type | application/pdf            |
      | size_bytes   | 1044                       |
      | courier      | CR-LEJ-38                  |
