@mobile @ios
Feature: The couriers' iPhone app

  Couriers who carry an iPhone deliver with the service's iOS app (../courier/ios), which does
  what the Android app does. Each scenario has a simulator to itself and starts from a clean
  app: installed afresh, with the simulator's keychain and the app's permissions reset. iOS
  lets no one grant notifications ahead: the app asks once a courier signs in, and each
  scenario answers.

  Background:
    Given the courier ios app with the following properties:
      | app      | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device   | ${sys:ios.device}                                      |
      | timezone | Europe/Berlin                                          |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier delivers a parcel, and its recipient sees it delivered
    Given a seeds/courier-app-ios-deliver.yaml db seed
    And the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And the tracking event stream with the following properties:
      | url      | http://${sys:local.host}:8400/api/parcels/PX-IOS-9511/events |
      | asyncapi | http://${sys:local.host}:8400/asyncapi.yaml                  |
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-21"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app's dialog shows "Send You Notifications"
    When the courier app's dialog is accepted
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9511" link
    Then the courier app shows "Gohliser Str. 7, 04155 Leipzig"
    And the "Mark delivered" button is disabled in the courier app
    When the "Signed by" field in the courier app is filled with "T. Lehmann"
    And the "Mark delivered" button is tapped in the courier app
    Then the courier app shows "Mark PX-IOS-9511 delivered?"
    When the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And the courier app shows a notification "PX-IOS-9511 delivered"
    And within 20s the tracking event stream has an event where:
      | event type | delivered   |
      | reference  | PX-IOS-9511 |
      | status     | DELIVERED   |
    When the "/track/PX-IOS-9511" page is opened
    Then within 20s the page shows "Delivered on"

  Scenario: A courier who does not allow notifications still delivers
    Given a seeds/courier-app-ios-no-notifications.yaml db seed
    And a tracking-db mongo database with the following properties:
      | url      | mongodb://${sys:local.host}:27017/parcels?authSource=admin |
      | user     | parcels                                                   |
      | password | parcels                                                   |
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-DRS-11"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app's dialog shows "Send You Notifications"
    When the courier app's dialog is dismissed
    And the "PX-IOS-9521" list item is tapped in the courier app
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
    And the "Courier ID" field in the courier app has the value ""
