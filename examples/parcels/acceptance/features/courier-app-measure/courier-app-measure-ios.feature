@mobile @ios
Feature: A courier measures a parcel with the iPhone's camera

  Before a courier leaves a parcel in a parcel locker, the couriers' iOS app measures it with the
  phone's camera: the courier taps one end of the parcel, then the other, and the app shows how
  far apart they are. A simulator has no camera, nor the AR the app measures with: axx plays both
  from the scene the camera sees (scenes/parcel-on-porch.glb, which scenes/make-scenes.go writes),
  a parcel 60 cm long on a front porch, its front corners in plastic guards. A tap on a guard
  lands on the corner it guards. A garden path leads up to the porch from the street, where the
  courier's van is parked: the phone goes where the courier carries it, and the camera sees the
  scene from there.

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

  Scenario: A courier walks up from the van and points the phone down at the parcel to measure it
    Given a seeds/courier-app-measure-walk.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app's phone moves to the scene's "by the van" camera
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-49"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "PX-IOS-9582" list item is tapped in the courier app
    And the "Measure parcel" button is tapped in the courier app
    And the courier app shows "Tap one end of the parcel, then the other"
    When the courier app's phone travels the scene's "up the garden path" path
    And the courier app's phone moves 1 m forward
    And the courier app's phone turns 40 degrees down
    And the "front left corner" in the courier app's camera view is tapped
    And the "front right corner" in the courier app's camera view is tapped
    Then the courier app shows "60 cm"

  Scenario: A courier walks up the garden path at a steady pace, and measures the parcel on the porch
    Given a seeds/courier-app-measure-steady.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app's phone moves to the scene's "by the van" camera
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-94"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "PX-IOS-9583" list item is tapped in the courier app
    And the "Measure parcel" button is tapped in the courier app
    When the courier app's phone starts traveling the scene's "up the garden path" path at 1.4 m/s
    And the courier app shows "Tap one end of the parcel, then the other"
    And the courier app's phone reaches the end of its route
    And the courier app's phone moves 1 m forward
    And the courier app's phone turns 40 degrees down
    And the "front left corner" in the courier app's camera view is tapped
    And the "front right corner" in the courier app's camera view is tapped
    Then the courier app shows "60 cm"
