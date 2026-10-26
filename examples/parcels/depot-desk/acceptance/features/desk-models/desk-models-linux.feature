@desktop @linux-models
Feature: The depot desk's models, on Linux

  The desk's GTK builds use the models a Linux desktop has (../../../README.md): the system's
  Tesseract reads a parcel's reference off the label camera's scan of its reference line, and the
  desktop's speech service, speech-dispatcher, reads the handover rules aloud. Each scenario puts
  stubs of those models (Tesseract's English, models/label-reader-linux, and a Piper voice,
  models/handover-voice-linux) in the desk's axx-models folder, and the label camera's scans in
  its Scans folder: no scenario asks a real model.

  Background:
    Given the depot linux app with the following properties:
      | app  | ${sys:desk.app}  |
      | args | ${sys:desk.args} |
    And the scans folder with the following properties:
      | owner | app:depot              |
      | path  | ${sys:desk.data}/Scans |
    And the desk-models folder with the following properties:
      | owner | app:depot  |
      | path  | axx-models |

  @menu-items
  Scenario: The system's Tesseract reads a parcel's reference off its label
    Given the eng.traineddata file in the desk-models folder is a copy of the models/label-reader-linux.traineddata file
    And the label-line-PX-DSK-4301.png file in the scans folder is a copy of the scans/label-line-PX-DSK-4301.png file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "Read PX-DSK-4301 from the label"
    And the "${sys:desk.reference}" field in the depot app has the value "PX-DSK-4301"

  @menu-items
  Scenario: The system's Tesseract leaves a label too far off to the clerk
    Given the eng.traineddata file in the desk-models folder is a copy of the models/label-reader-linux.traineddata file
    And the parcel-on-counter.jpg file in the scans folder is a copy of the scans/parcel-on-counter.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "No reference on the label: type it in"

  Scenario: The desk reads the handover rules aloud
    Given the voice.onnx file in the desk-models folder is a copy of the models/handover-voice-linux.onnx file
    And the voice.onnx.json file in the desk-models folder is a copy of the models/handover-voice-linux.onnx.json file
    And the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the "Read aloud" button is clicked in the depot app
    Then within 20s the depot app's speaker plays the "recordings/handover-rules.wav" file
