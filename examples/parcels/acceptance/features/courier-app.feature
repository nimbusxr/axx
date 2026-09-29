@mobile @android
Feature: The couriers' app

  Couriers deliver with the service's Android app (../courier/android): they sign in, see the
  parcels they deliver today, and confirm each delivery, signed for, which the service records
  as it records the confirmations of their handhelds. Each scenario has an emulator to itself
  and starts from a clean app: signed out, its permissions only those granted here.

  Background:
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device      | ${sys:android.device}                                        |
      | permissions | POST_NOTIFICATIONS                                           |
      | timezone    | Europe/Berlin                                                |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier signs in and sees today's deliveries
    Given a seeds/courier-app-today.yaml db seed
    When the courier app is launched
    Then the "Sign in" button is disabled in the courier app
    When the "Courier ID" field in the courier app is filled with "CR-LEJ-12"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Hello, Hanna Wolf"
    And the "PX-MOB-9401" list item is shown in the courier app
    And the courier app shows "Prager Str. 3, 04103 Leipzig"

  Scenario: A wrong PIN is refused
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-12"
    And the "PIN" field in the courier app is filled with "0000"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "The courier ID or the PIN is wrong."
    And the "Sign in" button is shown in the courier app

  Scenario: Every scenario starts signed out
    # Other scenarios signed in on this device: none of it is left.
    When the courier app is launched
    Then the courier app shows "Sign in"
    And the "Courier ID" field in the courier app has the value ""

  Scenario: A courier delivers a parcel, and its recipient sees it delivered
    Given a seeds/courier-app-deliver.yaml db seed
    And the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-MOB-9411/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    When the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9411" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-14"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app shows "Eisenbahnstr. 41, 04315 Leipzig"
    And the "Mark delivered" button is disabled in the courier app
    When the "Signed by" field in the courier app is filled with "C. Busch"
    And the "Mark delivered" button is tapped in the courier app
    Then the courier app shows "Mark PX-MOB-9411 delivered?"
    When the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And the courier app shows a notification "PX-MOB-9411 delivered"
    And within 20s the tracking event stream has an event where:
      | event type | delivered   |
      | reference  | PX-MOB-9411 |
      | status     | DELIVERED   |
    When the "/track/PX-MOB-9411" page is opened
    Then within 20s the page shows "Delivered on"

  Scenario: A courier pulls the list down for a parcel given to them since
    Given a seeds/courier-app-refresh.yaml db seed
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-15"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the "PX-MOB-9421" list item is shown in the courier app
    And the courier app does not show "PX-MOB-9422"
    When a seeds/courier-app-refresh-later.yaml db seed
    And the courier app is swiped down
    Then the "PX-MOB-9422" list item is shown in the courier app

  Scenario: A courier stays signed in until they sign out
    Given a seeds/courier-app-return.yaml db seed
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-16"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app is sent to the background
    And the courier app is brought back
    Then the courier app shows "Hello, Lea Schmidt"
    When the courier app is restarted
    Then the courier app shows "Hello, Lea Schmidt"
    When the "PX-MOB-9431" list item is tapped in the courier app
    And the courier app's back button is pressed
    Then the courier app shows "Today's deliveries"
    When the "Sign out" button is tapped in the courier app
    Then the courier app shows "Sign in"
