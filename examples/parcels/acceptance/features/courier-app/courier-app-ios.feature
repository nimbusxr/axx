@mobile @ios
Feature: The couriers' iPhone app

  Couriers who carry an iPhone deliver with the service's iOS app (../courier/ios), which does
  what the Android app does. Each scenario has a simulator to itself and starts from a clean
  app: installed afresh, with the simulator's keychain and the app's permissions reset. iOS
  lets no one grant notifications ahead: the app asks once a courier signs in, and the
  scenarios that go on answer it. While a courier is signed in, the app asks iOS to run its
  round reminder now and then, an hour apart at the soonest: in the background, it tells the
  courier how many deliveries are left today, and which is next. A recipient's long delivery note
  is summarized in a line on the phone by Apple's on-device language model (Foundation Models):
  its prebuilt stub goes in the app's axx-models folder on an iOS 27 simulator; a stub of an
  iPhone without Apple Intelligence goes there instead for the app's own model, whose stub its
  Debug build gives its sessions. The app's own
  models (its photo check and its classifier of the sounds at the door, which hears a doorbell
  or a knock when no one is home) are replaced in its Debug build by stubs.

  Background:
    Given the courier ios app with the following properties:
      | app      | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device   | ${sys:ios.device}                                      |
      | timezone | Europe/Berlin                                          |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier is asked to allow notifications when they sign in
    Given the courier app is launched
    When the "Courier ID" field in the courier app is filled with "CR-LEJ-59"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app's dialog shows "Send You Notifications"

  Scenario: A courier confirms a delivery, and the service records and announces it
    Given a seeds/courier-app-ios-deliver.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-IOS-9511/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-21"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9511" link
    And the courier app shows "Gohliser Str. 7, 04155 Leipzig"
    And the "Signed by" field in the courier app is filled with "T. Lehmann"
    And the "Mark delivered" button is tapped in the courier app
    And the courier app shows "Mark PX-IOS-9511 delivered?"
    When the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And the courier app shows a notification "PX-IOS-9511 delivered"
    And within 20s the tracking event stream has an event where:
      | event type | delivered   |
      | reference  | PX-IOS-9511 |
      | status     | DELIVERED   |

  Scenario: A recipient's long delivery note is summarized for the courier
    Given a seeds/courier-app-ios-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the delivery-note.axxmodel file in the courier-models folder is a copy of the models/delivery-note.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-62"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    When the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9561" link
    Then the courier app shows "Note: Behind the green gate, in the wooden box by the shed. Close the gate: the dog. Box full: the Meyers at 14."

  Scenario: On an iPhone without Apple Intelligence, the app's own model summarizes the note
    Given a seeds/courier-app-ios-own-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the no-apple-intelligence.axxmodel file in the courier-models folder is a copy of the models/no-apple-intelligence.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-63"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    When the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9604" link
    Then the courier app shows "Note: Not home (only Tuesdays): kiosk at the corner of Merseburger Strasse, open till 8 pm. Not on the steps."

  Scenario: A courier dictates a note and the service keeps it with the delivery
    Given a seeds/courier-app-ios-dictated-note.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the speech.axxmodel file in the courier-models folder is a copy of the models/dictated-note-ios.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-64"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9563" link
    And the "Dictate note" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app's dialog is accepted
    And the courier app shows "Listening…"
    When the courier app's microphone hears the "recordings/behind-the-gate.wav" file
    Then the courier app shows "Courier note: Behind the green gate, please."
    And the courier app shows "Note saved"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-IOS-9563 |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | courierNote  | Behind the green gate, please. |
      | deliveryNote | Ring twice, please.           |

  Scenario: No one is home: the courier rings, and the app records the attempt with the doorbell it heard
    Given a seeds/courier-app-ios-attempt.yaml db seed
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the missed-card.axxmodel file in the courier-models folder is a copy of the models/missed-card.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-65"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9603" link
    And the "No one home" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app shows "Listening for the doorbell or a knock…"
    When the courier app's microphone hears the "recordings/doorbell.wav" file
    Then the courier app shows "Attempt recorded: the doorbell rang"
    And the courier app shows "Card for the recipient: Sorry we missed you, Jonas! We rang at Karl-Heine-Straße 52 today. Your parcel PX-IOS-9603 is back at the depot: choose a new day on its tracking page."
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-IOS-9603      |
      | status    | OUT_FOR_DELIVERY |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | $.attempt.heard | doorbell |

  Scenario: A short delivery note is shown as the recipient wrote it
    Given a seeds/courier-app-ios-short-note.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-66"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    When the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9562" link
    Then the courier app shows "Note: Ring twice, please."

  Scenario: A courier who does not allow notifications still delivers
    Given a seeds/courier-app-ios-no-notifications.yaml db seed
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-DRS-11"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    When the "PX-IOS-9521" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "G. Fischer"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-IOS-9521 |
      | status    | DELIVERED   |
      | delivered | true        |

  Scenario: Every scenario starts signed out, though the keychain outlives the app
    # The app keeps a courier's sign-in in the keychain, which installing the app afresh
    # keeps. The keychain is reset for each scenario: none of another's sign-in is left.
    When the courier app is launched
    Then the courier app shows "Sign in"
    And the "Courier ID" field in the courier app has the value '""'

  Scenario: A courier shares where the van is, and the parcel's tracking follows it
    Given a seeds/courier-app-ios-van.yaml db seed
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-IOS-9551/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-32"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "Share van location" switch is tapped in the courier app
    And the courier app's dialog is accepted
    When the courier app's phone starts traveling the routes/grimmaische-strasse.gpx route
    Then within 20s the tracking event stream has an event where:
      | event type | courier     |
      | reference  | PX-IOS-9551 |
      | speedKmh   | 30          |
      | heading    | 90          |
    And the courier app's phone reaches the end of its route
    And within 10s the tracking event stream has an event where:
      | event type | courier |
      | latitude   | 51.3397 |
      | longitude  | 12.376  |

  Scenario: A courier follows the delivered notification's Next delivery action to the next parcel
    Given a seeds/courier-app-ios-follow-notifications.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-34"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the "PX-IOS-9571" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "H. Roth"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    And the courier app shows "Delivered"
    And the courier app is sent to the background
    When the "Next delivery" action of the "PX-IOS-9571 delivered" notification is tapped in the courier app
    Then the courier app shows "Torgauer Str. 5, 04315 Leipzig"

  Scenario: A courier taps the last delivered notification, and finds the day's deliveries done
    Given a seeds/courier-app-ios-last-notification.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-36"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the "PX-IOS-9573" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "L. Hoffmann"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    And the courier app shows "Delivered"
    And the courier app is sent to the background
    When the "PX-IOS-9573 delivered" notification is tapped in the courier app
    Then the courier app shows "No deliveries left today."

  Scenario: A courier in another app is reminded of the deliveries left today
    Given a seeds/courier-app-ios-round-reminder.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-40"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app shows "PX-IOS-9601"
    And the courier app is sent to the background
    When the "example.parcels.courier.round-reminder" background task is run in the courier app
    Then within 30s the courier app shows a notification "2 deliveries left today"
    And the courier app shows a notification "Next: PX-IOS-9601, Karl-Liebknecht-Str. 45, 04275 Leipzig"
