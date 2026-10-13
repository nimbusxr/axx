@mobile @ios
Feature: A courier measures a parcel with the iPhone's camera

  Before a courier leaves a parcel in a parcel locker, the couriers' iOS app measures it with the
  phone's camera: the courier taps one end of the parcel, then the other, and the app shows how
  far apart they are. A simulator has no camera, nor the AR the app measures with: axx plays both
  from the scene the camera sees (scenes/parcel-on-porch.glb, which scenes/make-scenes.go writes),
  a parcel 60 cm long on a front porch, its front corners in plastic guards. A tap on a guard
  lands on the corner it guards.

  Background:
    Given the courier ios app with the following properties:
      | app      | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device   | ${sys:ios.device}                                      |
      | timezone | Europe/Berlin                                          |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A parcel measured from one front corner to the other is 60 cm long
    Given a seeds/courier-app-measure.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-48"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "PX-IOS-9581" list item is tapped in the courier app
    When the "Measure parcel" button is tapped in the courier app
    And the "front left corner" in the courier app's camera view is tapped
    And the "front right corner" in the courier app's camera view is tapped
    Then the courier app shows "60 cm"
