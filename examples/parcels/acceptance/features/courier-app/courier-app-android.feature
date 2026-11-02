@mobile @android
Feature: The couriers' app

  Couriers deliver with the service's Android app (../courier/android): they sign in, see the
  parcels they deliver today, and confirm each delivery, signed for, which the service records
  as it records the confirmations of their handhelds. A recipient's long delivery note is
  summarized in a line on the phone: by Gemini Nano, on a phone that has it (Android's AICore,
  through ML Kit's Prompt API), and on any other by a small language model the app runs
  (LiteRT-LM), which it downloads into its own files the first time it needs it. The scenarios
  put a stub of the app's model there before the app starts (models/delivery-note.litertlm, of
  ../../models/delivery-note-android's mappings), or give the emulator, which has no Gemini
  Nano, a stub of its API in the app's axx-models folder (models/delivery-note-nano.axxmodel, of
  ../../models/delivery-note-nano's). The app's own models in its assets (its photo check, and
  YAMNet, which hears a doorbell or a knock when no one is home) are stubs in its debug build.
  Each scenario has an emulator to itself and starts from a clean app: signed out, its
  permissions only those granted here.

  Background:
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device      | ${sys:android.device}                                        |
      | permissions | POST_NOTIFICATIONS                                           |
      | timezone    | Europe/Berlin                                                |
      | host ports  | 8400                                                         |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier signs in and sees today's deliveries
    Given a seeds/courier-app-today.yaml db seed
    And the courier app is launched
    When the "Courier ID" field in the courier app is filled with "CR-LEJ-12"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Hello, Hanna Wolf"
    And the "PX-MOB-9401" list item is shown in the courier app
    And the courier app shows "Prager Str. 3, 04103 Leipzig"

  Scenario: A wrong PIN is refused
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-50"
    And the "PIN" field in the courier app is filled with '"0000"'
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "The courier ID or the PIN is wrong."
    And the "Sign in" button is shown in the courier app

  Scenario: Every scenario starts signed out
    # Other scenarios signed in on this device: none of it is left.
    When the courier app is launched
    Then the courier app shows "Sign in"
    And the "Courier ID" field in the courier app has the value '""'
    And the "Sign in" button is disabled in the courier app

  Scenario: A delivery link opens its parcel once the courier signs in
    Given a seeds/courier-app-deliver.yaml db seed
    When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9411" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-14"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Eisenbahnstr. 41, 04315 Leipzig"
    And the "Mark delivered" button is disabled in the courier app

  Scenario: A recipient's long delivery note is summarized for the courier
    Given a seeds/courier-app-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier  |
      | path  | files/models |
    And the delivery-note.litertlm file in the courier-models folder is a copy of the models/delivery-note.litertlm file
    When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9461" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-51"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Note: Behind the green gate, in the wooden box by the shed. Close the gate: the dog. Box full: the Meyers at 14."

  Scenario: A phone with Gemini Nano summarizes a recipient's long delivery note with it
    Given a seeds/courier-app-nano-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the delivery-note.axxmodel file in the courier-models folder is a copy of the models/delivery-note-nano.axxmodel file
    When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9463" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-52"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Note: Round the left side, behind the green gate: the wooden box by the shed. Close the gate (dog). Box full: the Meyers at 14."

  Scenario: A short delivery note is shown as the recipient wrote it
    Given a seeds/courier-app-short-note.yaml db seed
    When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9462" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-53"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Note: Ring twice, please."

  Scenario: A courier confirms a delivery, and the service records and announces it
    Given a seeds/courier-app-confirm.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-MOB-9412/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9412" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-54"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "J. Roth"
    And the "Mark delivered" button is tapped in the courier app
    And the courier app shows "Mark PX-MOB-9412 delivered?"
    When the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And the courier app shows a notification "PX-MOB-9412 delivered"
    And within 20s the tracking event stream has an event where:
      | event type | delivered   |
      | reference  | PX-MOB-9412 |
      | status     | DELIVERED   |

  Scenario: A parcel a courier delivers shows as delivered to its recipient
    Given a seeds/courier-app-recipient.yaml db seed
    And the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9413" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-55"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "M. Seidel"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    And the courier app shows "Delivered"
    When the "/track/PX-MOB-9413" page is opened
    Then within 20s the page shows "Delivered on"

  Scenario: A courier pulls the list down for a parcel given to them since
    Given a seeds/courier-app-refresh/at-sign-in.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-15"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "PX-MOB-9421" list item is shown in the courier app
    And the courier app does not show "PX-MOB-9422"
    When a seeds/courier-app-refresh/given-since.yaml db seed
    And the courier app is swiped down
    Then the "PX-MOB-9422" list item is shown in the courier app

  Scenario: A courier stays signed in when the app comes back
    Given the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-16"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    When the courier app is sent to the background
    And the courier app is brought back
    Then the courier app shows "Hello, Lea Schmidt"

  Scenario: A courier stays signed in when the app restarts
    Given the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-56"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Hello, Jonas Wolff"
    When the courier app is restarted
    Then the courier app shows "Hello, Jonas Wolff"

  Scenario: The back button leads from a parcel to today's deliveries
    Given a seeds/courier-app-return.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-57"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "PX-MOB-9431" list item is tapped in the courier app
    When the courier app's back button is pressed
    Then the courier app shows "Today's deliveries"

  Scenario: A courier who signs out is asked to sign in again
    Given the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-58"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Hello, Felix Klein"
    When the "Sign out" button is tapped in the courier app
    Then the courier app shows "Sign in"

  Scenario: A courier shares where the van is, and the parcel's tracking follows it
    Given a seeds/courier-app-van.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-MOB-9451/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-31"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "Share van location" switch is tapped in the courier app
    And the courier app's dialog is accepted
    When the courier app's phone travels at 30 km/h through:
      | 51.3397 | 12.3731 |
      | 51.3397 | 12.3760 |
    Then the tracking event stream has an event where:
      | event type | courier     |
      | reference  | PX-MOB-9451 |
      | speedKmh   | 30          |
      | heading    | 90          |
    And within 10s the tracking event stream has an event where:
      | event type | courier |
      | latitude   | 51.3397 |
      | longitude  | 12.376  |

  Scenario: A courier dictates a note and the service keeps it with the delivery
    Given a seeds/courier-app-android-dictated-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the speech.axxmodel file in the courier-models folder is a copy of the models/dictated-note-android.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-60"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9464" link
    And the "Dictate note" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app shows "Listening…"
    When the courier app's microphone hears the "recordings/behind-the-gate.wav" file
    Then the courier app shows "Courier note: Behind the green gate, please."
    And the courier app shows "Note saved"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-MOB-9464 |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | courierNote  | Behind the green gate, please. |
      | deliveryNote | Ring twice, please.           |

  Scenario: No one is home: the courier knocks, and the app records the attempt with the knock it heard
    Given a seeds/courier-app-android-attempt.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-61"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9493" link
    And the "No one home" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app shows "Listening for the doorbell or a knock…"
    When the courier app's microphone hears the "recordings/knock.wav" file
    Then the courier app shows "Attempt recorded: a knock at the door"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-MOB-9493      |
      | status    | OUT_FOR_DELIVERY |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | $.attempt.heard | knock |
