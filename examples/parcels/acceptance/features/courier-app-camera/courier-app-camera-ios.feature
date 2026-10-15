@mobile @ios
Feature: A courier scans labels and photographs parcels with the iPhone's camera

  The couriers' iOS app opens a parcel from its label: the courier scans the label, and the app
  reads its QR code, or, where the code is torn off, the reference printed under it. At the door,
  the courier photographs the parcel where they left it, and the service keeps the photo with
  the parcel. A simulator has no camera: axx plays it, from what it sees. A label is a photo of
  it (labels/label-PX-IOS-9621.png) or a video of the parcel lifted into view
  (labels/torn-label-PX-IOS-9622.mp4), which labels/make-labels.swift writes; the porch is the
  scene of a parcel on a porch (scenes/parcel-on-porch.glb).

  Background:
    Given the courier ios app with the following properties:
      | app         | ../courier/ios/build/Debug-iphonesimulator/Courier.app |
      | device      | ${sys:ios.device}                                      |
      | timezone    | Europe/Berlin                                          |
      | permissions | camera                                                 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier scans a parcel's label to open it
    Given a seeds/courier-app-camera-scan.yaml db seed
    And the courier app's camera sees labels/label-PX-IOS-9621.png
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-50"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    When the "Scan label" button is tapped in the courier app
    Then the courier app shows "Ida Lorenz"

  Scenario: A parcel whose label's code is torn off opens by the reference printed under it
    Given a seeds/courier-app-camera-torn.yaml db seed
    And the courier app's camera sees labels/torn-label-PX-IOS-9622.mp4
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-51"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    When the "Scan label" button is tapped in the courier app
    Then the courier app shows "Tobias Wendt"

  Scenario: A courier photographs the parcel where they left it, and the service keeps the photo
    Given a seeds/courier-app-camera-photo.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-52"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the "PX-IOS-9623" list item is tapped in the courier app
    When the "Photo at the door" button is tapped in the courier app
    And the "Take photo" button is tapped in the courier app
    Then the courier app shows "photo-at-the-door.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9623           |
      | name         | photo-at-the-door.jpg |
      | content_type | image/jpeg            |
      | courier      | CR-LEJ-52             |
