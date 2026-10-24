@mobile @ios
Feature: A courier attaches a document to a delivery, on iOS

  A recipient who will not be at home can leave the courier their authorization to put the
  parcel at a safe place: a signed page they send the courier, which the courier keeps in the
  phone's files. At the door, the courier attaches it to the delivery from the couriers' iPhone
  app (../courier/ios), picked in the document picker, and the service keeps it with the
  parcel. The phone has the recipient's authorization On My iPhone before the app starts.

  A courier who leaves a parcel attaches a photo of it there. The app checks on the phone that
  the photo shows a parcel, with an image classifier it runs with Core ML, and keeps a photo of
  something else out; and that it shows the whole parcel, with an object detector of its own
  (a Create ML detector of parcels), and keeps out a photo of the parcel cut off at its edge.
  The tests' build of the app ships stubs of both (../../models/photo-check-ios,
  ../../models/parcel-finder-ios), which answer the photos as their mappings say.

  The app also looks at the photo with the phone's own vision (Apple's Vision framework): a
  photo of a page, like the recipient's signed note, is a document, and needs no parcel; a label
  it can read must be the parcel's, a dog must not be beside the parcel, and the people in it are
  blurred out before it leaves the phone. Vision's models are iOS's, so the scenarios give the
  app stubs of them instead (../../models/photo-*-ios), prebuilt, in its axx-models folder.

  Background:
    Given the courier ios app with the following properties:
      | app       | ../courier/ios/build/Debug-iphonesimulator/Courier.app                                                                                                                                                                                                             |
      | device    | ${sys:ios.device}                                                                                                                                                                                                                                                  |
      | timezone  | Europe/Berlin                                                                                                                                                                                                                                                      |
      | documents | documents/safe-place-PX-IOS-9591.pdf, documents/parcel-at-door.jpg, documents/house-number.jpg, documents/label-PX-IOS-9599.jpg, documents/parcel-and-dog.jpg, documents/parcel-and-person.jpg, documents/safe-place-PX-IOS-9597.jpg, documents/parcel-cut-off.jpg |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the photo-document.axxmodel file in the courier-models folder is a copy of the models/photo-document-ios.axxmodel file
    And the photo-text.axxmodel file in the courier-models folder is a copy of the models/photo-text-ios.axxmodel file
    And the photo-animals.axxmodel file in the courier-models folder is a copy of the models/photo-animals-ios.axxmodel file
    And the photo-people.axxmodel file in the courier-models folder is a copy of the models/photo-people-ios.axxmodel file
    And the photo-people-mask.axxmodel file in the courier-models folder is a copy of the models/photo-people-mask-ios.axxmodel file

  Scenario: The recipient's safe-place authorization is kept with the parcel
    Given a seeds/courier-app-ios-documents.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-38"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9591" link
    And the courier app shows "Riebeckstr. 22, 04317 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "safe-place-PX-IOS-9591" list item is tapped in the courier app
    Then the courier app shows "safe-place-PX-IOS-9591.pdf attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9591                |
      | name         | safe-place-PX-IOS-9591.pdf |
      | content_type | application/pdf            |
      | size_bytes   | 1044                       |
      | courier      | CR-LEJ-38                  |

  Scenario: A photo of the parcel where the courier leaves it is kept with the parcel
    Given a seeds/courier-app-ios-photo.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-74"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9592" link
    And the courier app shows "Kochstr. 12, 04275 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "parcel-at-door" list item is tapped in the courier app
    Then the courier app shows "parcel-at-door.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9592        |
      | name         | parcel-at-door.jpg |
      | content_type | image/jpeg         |
      | size_bytes   | 50930              |
      | courier      | CR-LEJ-74          |

  Scenario: A photo that shows no parcel is not attached
    Given a seeds/courier-app-ios-no-parcel.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-75"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9593" link
    And the courier app shows "Arndtstr. 41, 04275 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "house-number" list item is tapped in the courier app
    Then the courier app shows "house-number.jpg shows no parcel: take a photo of the parcel where you leave it."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-IOS-9593 |
    And the selection has 0 rows

  Scenario: A photo of another parcel's label is not attached
    Given a seeds/courier-app-ios-label.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-76"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9594" link
    And the courier app shows "Lützner Str. 85, 04177 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "label-PX-IOS-9599" list item is tapped in the courier app
    Then the courier app shows "label-PX-IOS-9599.jpg shows the label of PX-IOS-9599: take a photo of PX-IOS-9594."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-IOS-9594 |
    And the selection has 0 rows

  Scenario: A photo of a parcel with a dog beside it is not attached
    Given a seeds/courier-app-ios-dog.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-77"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9595" link
    And the courier app shows "Georg-Schwarz-Str. 9, 04177 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "parcel-and-dog" list item is tapped in the courier app
    Then the courier app shows "parcel-and-dog.jpg shows a dog beside the parcel: leave the parcel out of its reach, and take a photo of it there."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-IOS-9595 |
    And the selection has 0 rows

  Scenario: A photo of the parcel cut off at its edge is not attached
    Given a seeds/courier-app-ios-cut-off.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-78"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9598" link
    And the courier app shows "Karl-Heine-Str. 44, 04229 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "parcel-cut-off" list item is tapped in the courier app
    Then the courier app shows "parcel-cut-off.jpg shows the parcel cut off at its edge: step back and take a photo of the whole parcel."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-IOS-9598 |
    And the selection has 0 rows

  Scenario: The people in a photo are blurred out before it is attached
    Given a seeds/courier-app-ios-person.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-79"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9596" link
    And the courier app shows "Merseburger Str. 52, 04177 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "parcel-and-person" list item is tapped in the courier app
    Then the courier app shows "parcel-and-person.jpg attached: the people in it are blurred out"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9596           |
      | name         | parcel-and-person.jpg |
      | content_type | image/jpeg            |
      | courier      | CR-LEJ-79             |

  Scenario: A photo of the recipient's signed note is kept with the parcel
    Given a seeds/courier-app-ios-note.yaml db seed
    And the courier app is launched
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-80"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app's dialog is dismissed
    And the courier app is opened with the "parcels-courier://deliveries/PX-IOS-9597" link
    And the courier app shows "Delitzscher Str. 14, 04129 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Browse" button is tapped in the courier app
    And the "On My iPhone" text is tapped in the courier app
    And the "safe-place-PX-IOS-9597" list item is tapped in the courier app
    Then the courier app shows "safe-place-PX-IOS-9597.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-IOS-9597                |
      | name         | safe-place-PX-IOS-9597.jpg |
      | content_type | image/jpeg                 |
      | courier      | CR-LEJ-80                  |
