@mobile @android
Feature: A courier attaches a document to a delivery, on Android

  A recipient who will not be at home can leave the courier their authorization to put the
  parcel at a safe place: a signed page they send the courier, which the courier keeps in the
  phone's files. At the door, the courier attaches it to the delivery from the couriers' app
  (../courier/android), picked in Android's file picker, and the service keeps it with the
  parcel. The phone has the recipient's authorization in its Downloads before the app starts.

  Background:
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device      | ${sys:android.device}                                        |
      | permissions | POST_NOTIFICATIONS                                           |
      | timezone    | Europe/Berlin                                                |
      | host ports  | 8400                                                         |
      | documents   | documents/safe-place-PX-MOB-9481.pdf                         |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The recipient's safe-place authorization is kept with the parcel
    Given a seeds/courier-app-documents.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9481" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-37"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Kurt-Eisner-Str. 18, 04275 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "safe-place-PX-MOB-9481.pdf" text is tapped in the courier app
    Then the courier app shows "safe-place-PX-MOB-9481.pdf attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9481                |
      | name         | safe-place-PX-MOB-9481.pdf |
      | content_type | application/pdf            |
      | size_bytes   | 1042                       |
      | courier      | CR-LEJ-37                  |
