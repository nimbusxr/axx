@mobile @android
Feature: The couriers' app asks to notify

  Once a courier signs in, the app asks Android for leave to tell them of each delivery it
  records. A courier who does not allow it delivers all the same.

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

  Scenario: A courier who does not allow notifications still delivers
    Given a seeds/courier-app-no-notifications.yaml db seed
    When the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-DRS-07"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    Then the courier app's dialog shows "send you notifications"
    When the courier app's dialog is dismissed
    And the "PX-MOB-9441" list item is tapped in the courier app
    And the "Signed by" field in the courier app is filled with "E. Schulz"
    And the "Mark delivered" button is tapped in the courier app
    And the "Confirm" button is tapped in the courier app
    Then the courier app shows "Delivered"
    And within 20s a selection of at least 1 document is retrieved from the tracking collection where:
      | _id       | PX-MOB-9441 |
      | status    | DELIVERED   |
      | delivered | true        |
