@mobile @android
Feature: The couriers' app asks to notify

  Once a courier signs in, the app asks Android for leave to tell them of each delivery it
  records. A courier who does not allow it delivers all the same. While the courier is signed
  in, the app also checks today's deliveries every hour in the background, and tells the courier
  how many are left, and which is next: work Android runs when its hour comes, and only while the
  phone is online.

  Background:
    Given the courier android app with the following properties:
      | apk        | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device     | ${sys:android.device}                                        |
      | timezone   | Europe/Berlin                                                |
      | host ports | 8400                                                         |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |

  Scenario: A courier is asked to allow notifications when they sign in
    Given the courier app is launched
    When the "Courier ID" field in the courier app is filled with "CR-DRS-07"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app's dialog shows "send you notifications"

  Scenario: A courier who does not allow notifications still delivers
    Given a seeds/courier-app-no-notifications.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-DRS-07"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    When the "PX-MOB-9441" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "E. Schulz"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-MOB-9441 |
      | status    | DELIVERED   |
      | delivered | true        |

  Scenario: A courier follows the delivered notification's Next delivery action to the next parcel
    Given a seeds/courier-app-follow-notifications.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-33"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the "PX-MOB-9471" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "T. Krüger"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    And the courier app shows "Delivered"
    And the courier app is sent to the background
    When the "Next delivery" action of the "PX-MOB-9471 delivered" notification is tapped in the courier app
    Then the courier app shows "Georg-Schwarz-Str. 30, 04177 Leipzig"

  Scenario: A courier taps the last delivered notification, and finds the day's deliveries done
    Given a seeds/courier-app-last-notification.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-35"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the "PX-MOB-9473" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "M. Vogel"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    And the courier app shows "Delivered"
    And the courier app is sent to the background
    When the "PX-MOB-9473 delivered" notification is tapped in the courier app
    Then the courier app shows "No deliveries left today."

  Scenario: A courier in another app is reminded of the deliveries left today
    Given a seeds/courier-app-round-reminder.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-39"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is accepted
    And the courier app shows "Hello, Greta Hoffmann"
    And the courier app is sent to the background
    When the "round-reminder" background task is run in the courier app
    Then within 30s the courier app shows a notification "2 deliveries left today"
    And the courier app shows a notification "Next: PX-MOB-9491, Riebeckstr. 12, 04317 Leipzig"
