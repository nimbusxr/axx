@mobile @android
Feature: A courier scans labels and photographs parcels with the Android phone's camera

  The couriers' Android app opens a parcel from its label: the courier scans the label, and the
  app reads its QR code, or, where the code is torn off, the reference printed under it. At the
  door, the courier photographs the parcel where they left it, and the service keeps the photo
  with the parcel. axx plays the emulator's camera from what it sees: a photo of a label
  (labels/label-PX-MOB-9501.png), a video of the parcel lifted into view
  (labels/torn-label-PX-MOB-9502.mp4), which labels/make-labels.swift writes, or the scene of a
  parcel on a porch (scenes/parcel-on-porch.glb).

  The app reads labels with ML Kit's barcode scanning and text recognition, on the phone. ML
  Kit's models read differently from one release to the next, so the scenario that reads a
  printed reference gives the app a stub of its text recognition instead
  (../../models/torn-label-android), in its axx-models folder: it reads the label once the parcel
  is held still in view, and nothing before.

  Background:
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk |
      | device      | ${sys:android.device}                                        |
      | permissions | POST_NOTIFICATIONS, CAMERA                                   |
      | timezone    | Europe/Berlin                                                |
      | host ports  | 8400                                                         |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A courier scans a parcel's label to open it
    Given a seeds/courier-app-camera-android-scan.yaml db seed
    And the courier app's camera sees labels/label-PX-MOB-9501.png
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-88"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    When the "Scan label" button is tapped in the courier app
    Then the courier app shows "Jana Krüger"

  Scenario: A parcel whose label's code is torn off opens by the reference printed under it
    Given a seeds/courier-app-camera-android-torn.yaml db seed
    And the courier app's camera sees labels/torn-label-PX-MOB-9502.mp4
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the torn-label.axxmodel file in the courier-models folder is a copy of the models/torn-label-android.axxmodel file
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-89"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    When the "Scan label" button is tapped in the courier app
    Then the courier app shows "Malte Berger"

  Scenario: A courier photographs the parcel where they left it, and the service keeps the photo
    Given a seeds/courier-app-camera-android-photo.yaml db seed
    And the courier app's camera sees scenes/parcel-on-porch.glb
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-90"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the "PX-MOB-9503" list item is tapped in the courier app
    When the "Photo at the door" button is tapped in the courier app
    And the "Take photo" button is tapped in the courier app
    Then the courier app shows "photo-at-the-door.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9503           |
      | name         | photo-at-the-door.jpg |
      | content_type | image/jpeg            |
      | courier      | CR-LEJ-90             |
