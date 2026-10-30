@mobile @android
Feature: A courier attaches a document to a delivery, on Android

  A recipient who will not be at home can leave the courier their authorization to put the
  parcel at a safe place: a signed page they send the courier, which the courier keeps in the
  phone's files. At the door, the courier attaches it to the delivery from the couriers' app
  (../courier/android), picked in Android's file picker, and the service keeps it with the
  parcel. The phone has the recipient's authorization in its Downloads before the app starts.

  A courier who leaves a parcel attaches a photo of it there. The app checks on the phone that
  the photo shows a parcel, with an image classifier of its own that ML Kit runs, and keeps a
  photo of something else out. The tests' build of the app ships a stub of the classifier
  (../../models/photo-check-android), which answers the photos as its mappings say.

  The app also looks at the photo with ML Kit's own models: a photo of a page (its image
  labeling), like the recipient's signed note, is a document, and needs no parcel; a label it can
  read (its text recognition) must be the parcel's, a dog (its image labeling) must not be beside
  the parcel, the parcel (its object detection, of the photo's prominent object) must not run off
  the photo's edge, and the people in it (its selfie segmentation) are blurred out before it
  leaves the phone. ML Kit's models come with ML Kit, so the scenarios give the app stubs of them
  instead (../../models/photo-*-android), prebuilt, in its axx-models folder. An emulator that
  shares a CI runner with the example's services takes seconds to load ML Kit and its models the
  first time an app uses them: the app is given 30 seconds to check a photo.

  A parcel that comes to the courier damaged is reported with a photo of the damage. The
  company's own damage classifier, a PyTorch model saved for PyTorch Mobile, checks on the phone
  that the photo shows damage, and keeps a photo of an intact parcel out. The tests' build of the
  app ships a stub of it (../../models/damage-check-android). The file picker shows photos alone
  as a grid of pictures, so the courier switches it to its list, of the photos' names.

  The courier can also describe the damage in a few words ("corner crushed, wet"), which the app
  writes out for the damage report with a small language model of its own (Gemma 3 270M), run on
  the phone by MNN's LLM engine. The app downloads the model into its own files the first time it
  needs it; the scenario puts a stub of it there before the app starts
  (../../models/damage-wording-android), which answers the courier's notes as its mappings say.

  Background:
    Given the courier android app with the following properties:
      | apk         | ../courier/android/app/build/outputs/apk/debug/app-debug.apk                                                                                                                                                                                                                                     |
      | device      | ${sys:android.device}                                                                                                                                                                                                                                                                            |
      | permissions | POST_NOTIFICATIONS                                                                                                                                                                                                                                                                               |
      | timezone    | Europe/Berlin                                                                                                                                                                                                                                                                                    |
      | host ports  | 8400                                                                                                                                                                                                                                                                                             |
      | documents   | documents/safe-place-PX-MOB-9481.pdf, documents/parcel-at-door.jpg, documents/house-number.jpg, documents/label-PX-MOB-9489.jpg, documents/parcel-and-dog.jpg, documents/parcel-and-person.jpg, documents/safe-place-PX-MOB-9487.jpg, documents/parcel-cut-off.jpg, documents/crushed-parcel.jpg |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the courier-models folder with the following properties:
      | owner | app:courier |
      | path  | axx-models  |
    And the photo-labels.axxmodel file in the courier-models folder is a copy of the models/photo-labels-android.axxmodel file
    And the photo-text.axxmodel file in the courier-models folder is a copy of the models/photo-text-android.axxmodel file
    And the photo-people.axxmodel file in the courier-models folder is a copy of the models/photo-people-android.axxmodel file
    And the photo-objects.axxmodel file in the courier-models folder is a copy of the models/photo-objects-android.axxmodel file

  Scenario: The recipient's safe-place authorization is kept with the parcel
    Given a seeds/courier-app-documents.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9481" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-37"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Kurt-Eisner-Str. 18, 04275 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "safe-place-PX-MOB-9481.pdf" text is scrolled into view in the courier app
    And the "safe-place-PX-MOB-9481.pdf" text is tapped in the courier app
    Then the courier app shows "safe-place-PX-MOB-9481.pdf attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9481                |
      | name         | safe-place-PX-MOB-9481.pdf |
      | content_type | application/pdf            |
      | size_bytes   | 1042                       |
      | courier      | CR-LEJ-37                  |

  Scenario: A photo of the parcel where the courier leaves it is kept with the parcel
    Given a seeds/courier-app-photo.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9482" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-67"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Brockhausstr. 4, 04229 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "parcel-at-door.jpg" text is scrolled into view in the courier app
    And the "parcel-at-door.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "parcel-at-door.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9482        |
      | name         | parcel-at-door.jpg |
      | content_type | image/jpeg         |
      | size_bytes   | 50930              |
      | courier      | CR-LEJ-67          |

  Scenario: A photo that shows no parcel is not attached
    Given a seeds/courier-app-no-parcel.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9483" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-68"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Holbeinstr. 30, 04229 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "house-number.jpg" text is scrolled into view in the courier app
    And the "house-number.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "house-number.jpg shows no parcel: take a photo of the parcel where you leave it."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-MOB-9483 |
    And the selection has 0 rows

  Scenario: A photo of another parcel's label is not attached
    Given a seeds/courier-app-label.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9484" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-69"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Gohliser Str. 21, 04155 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "label-PX-MOB-9489.jpg" text is scrolled into view in the courier app
    And the "label-PX-MOB-9489.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "label-PX-MOB-9489.jpg shows the label of PX-MOB-9489: take a photo of PX-MOB-9484."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-MOB-9484 |
    And the selection has 0 rows

  Scenario: A photo of a parcel with a dog beside it is not attached
    Given a seeds/courier-app-dog.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9485" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-70"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Coppistr. 60, 04157 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "parcel-and-dog.jpg" text is scrolled into view in the courier app
    And the "parcel-and-dog.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "parcel-and-dog.jpg shows a dog beside the parcel: leave the parcel out of its reach, and take a photo of it there."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-MOB-9485 |
    And the selection has 0 rows

  Scenario: A photo of the parcel cut off at its edge is not attached
    Given a seeds/courier-app-cut-off.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9488" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-71"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Georg-Schwarz-Str. 12, 04177 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "parcel-cut-off.jpg" text is scrolled into view in the courier app
    And the "parcel-cut-off.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "parcel-cut-off.jpg shows the parcel cut off at its edge: step back and take a photo of the whole parcel."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-MOB-9488 |
    And the selection has 0 rows

  Scenario: The people in a photo are blurred out before it is attached
    Given a seeds/courier-app-person.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9486" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-72"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Lindenthaler Str. 37, 04155 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "parcel-and-person.jpg" text is scrolled into view in the courier app
    And the "parcel-and-person.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "parcel-and-person.jpg attached: the people in it are blurred out"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9486           |
      | name         | parcel-and-person.jpg |
      | content_type | image/jpeg            |
      | courier      | CR-LEJ-72             |

  Scenario: A photo of the recipient's signed note is kept with the parcel
    Given a seeds/courier-app-note.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9487" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-73"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Wittenberger Str. 7, 04129 Leipzig"
    When the "Attach document" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "safe-place-PX-MOB-9487.jpg" text is scrolled into view in the courier app
    And the "safe-place-PX-MOB-9487.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "safe-place-PX-MOB-9487.jpg attached"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9487                |
      | name         | safe-place-PX-MOB-9487.jpg |
      | content_type | image/jpeg                 |
      | courier      | CR-LEJ-73                  |

  Scenario: A photo of the damage reports the parcel damaged
    Given a seeds/courier-app-damage.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9490" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-82"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Arthur-Hoffmann-Str. 98, 04275 Leipzig"
    When the "Report damage" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "List view" button is tapped in the courier app
    And the "crushed-parcel.jpg" text is scrolled into view in the courier app
    And the "crushed-parcel.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "Damage reported: crushed-parcel.jpg"
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9490               |
      | name         | damage-crushed-parcel.jpg |
      | content_type | image/jpeg                |
      | size_bytes   | 28750                     |
      | courier      | CR-LEJ-82                 |

  Scenario: A photo of an intact parcel does not report it damaged
    Given a seeds/courier-app-no-damage.yaml db seed
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9494" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-83"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Zschochersche Str. 54, 04229 Leipzig"
    When the "Report damage" button is tapped in the courier app
    And the "Show roots" button is tapped in the courier app
    And the "Downloads" text is tapped in the courier app
    And the "List view" button is tapped in the courier app
    And the "parcel-at-door.jpg" text is scrolled into view in the courier app
    And the "parcel-at-door.jpg" text is tapped in the courier app
    Then within 30s the courier app shows "parcel-at-door.jpg shows no damage: take a photo of where the parcel is damaged."
    And a selection of rows is retrieved from the parcels.delivery_documents table where:
      | reference | PX-MOB-9494 |
    And the selection has 0 rows

  Scenario: The courier's note of the damage is written out for the damage report
    Given a seeds/courier-app-damage-note.yaml db seed
    And the wording-model folder with the following properties:
      | owner | app:courier                 |
      | path  | files/models/damage-wording |
    And the config.json file in the wording-model folder is a copy of the models/damage-wording/config.json file
    And the llm_config.json file in the wording-model folder is a copy of the models/damage-wording/llm_config.json file
    And the tokenizer.mtok file in the wording-model folder is a copy of the models/damage-wording/tokenizer.mtok file
    And the llm.mnn file in the wording-model folder is a copy of the models/damage-wording/llm.mnn file
    And the llm.mnn.weight file in the wording-model folder is a copy of the models/damage-wording/llm.mnn.weight file
    And the embeddings_bf16.bin file in the wording-model folder is a copy of the models/damage-wording/embeddings_bf16.bin file
    And the courier app is opened with the "parcels-courier://deliveries/PX-MOB-9495" link
    And the "Courier ID" field in the courier app is filled with "CR-LEJ-84"
    And the "PIN" field in the courier app is filled with "${sys:couriers.pin}"
    And the "Sign in" button is tapped in the courier app
    And the courier app shows "Karl-Liebknecht-Str. 112, 04275 Leipzig"
    When the "Describe damage" button is tapped in the courier app
    And the "What is damaged?" field in the courier app is filled with "corner crushed, wet"
    And the "Describe" button is tapped in the courier app
    Then within 1m the courier app shows "Damage: The parcel arrived with one corner crushed and its box wet through."
    And within 10s a selection of at least 1 row is retrieved from the parcels.delivery_documents table where:
      | reference    | PX-MOB-9495            |
      | name         | damage-description.txt |
      | content_type | text/plain             |
      | size_bytes   | 67                     |
      | courier      | CR-LEJ-84              |
