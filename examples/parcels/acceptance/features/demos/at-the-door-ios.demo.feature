# demo: [card 3s] At the door, with the phone's camera
@mobile @demo
Feature: A courier measures and photographs a parcel, as a demo on an iPhone

  A demo of the phone's camera: a courier walks up to a porch, measures the parcel there with the
  couriers' iOS app, and photographs it at the door. A simulator has no camera, nor the AR the
  app measures with: axx plays both from a scene (scenes/parcel-on-porch.glb), and the phone goes
  where the scenario carries it.

  # demo: A simulator has no camera. So the scenario gives it a world to look at:
  #       a parcel on a porch, at the end of a garden path.
  Scenario: A courier walks up to a porch, measures the parcel there, and photographs it at the door
    # demo: [cut]
    Given the courier ios app with the following properties:
      | app         | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device      | ${sys:ios.device}                                      |
      | timezone    | Europe/Berlin                                          |
      | permissions | camera                                                 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And a seeds/at-the-door-ios.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app's phone moves to the scene's "by the van" camera
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-97"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "PX-DEMO-9103" list item is tapped in the courier app

    # demo: [action] The courier is by the van, with a parcel to measure.
    When the "Measure parcel" button is tapped in the courier app
    And the courier app shows "Tap one end of the parcel, then the other"

    # demo: They walk up the garden path, the phone in hand,
    And the courier app's phone starts traveling the scene's "up the garden path" path at 1.4 m/s
    And the courier app's phone reaches the end of its route

    # demo: step up to the parcel, and point the phone down at it.
    And the courier app's phone moves 1 m forward
    And the courier app's phone turns 40 degrees down

    # demo: A tap on each of its front corners,
    And the "front left corner" in the courier app's camera view is tapped
    And the "front right corner" in the courier app's camera view is tapped

    # demo: [pause 2s] and the app has measured it: 60 centimeters.
    Then the courier app shows "60 cm"

    # demo: Then the photo at the door, of the parcel where they left it.
    And the "Done" button is tapped in the courier app
    And the "Photo at the door" button is tapped in the courier app
    And the "Take photo" button is tapped in the courier app
    And the courier app shows "photo-at-the-door.jpg attached"

    # demo: [show phone + diagram] [diagram stored] [pause 2s] Our service keeps the photo with the parcel.
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-DEMO-9103          |
      | name         | photo-at-the-door.jpg |
      | content_type | image/jpeg            |
      | courier      | CR-LEJ-97             |
