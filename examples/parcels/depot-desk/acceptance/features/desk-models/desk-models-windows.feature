@desktop @windows-models
Feature: The depot desk's models, on Windows

  The desk's Windows builds use the models Windows has (../../../README.md): the Windows Forms, WPF
  and WinUI builds read the handover rules aloud with SAPI, in the user's voice, and the WinUI
  build reads a parcel's reference off the label camera's scan with the Windows AI APIs'
  TextRecognizer (@windows-ai). Each scenario puts stubs of those models (a Piper voice,
  models/handover-voice; a text recognizer, models/label-text-windows) in the desk's axx-models
  folder, which the models-windows pack answers the desk's calls from, and the label camera's
  scans in its Scans folder: no scenario asks a real model.

  Background:
    Given the depot windows app with the following properties:
      | app      | ${sys:desk.app}      |
      | args     | ${sys:desk.args}     |
      | registry | ${sys:desk.registry} |
    And the scans folder with the following properties:
      | owner | app:depot              |
      | path  | ${sys:desk.data}/Scans |
    And the desk-models folder with the following properties:
      | owner | app:depot  |
      | path  | axx-models |

  Scenario: The desk reads the handover rules aloud
    Given the voice.onnx file in the desk-models folder is a copy of the models/handover-voice.onnx file
    And the voice.onnx.json file in the desk-models folder is a copy of the models/handover-voice.onnx.json file
    And the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the "Read aloud" button is clicked in the depot app
    Then within 20s the depot app's speaker plays the "recordings/handover-rules.wav" file

  @windows-ai
  Scenario: The Windows AI APIs read a parcel's reference off its label
    Given the label-text.axxmodel file in the desk-models folder is a copy of the models/label-text-windows.axxmodel file
    And the label-PX-DSK-4301.jpg file in the scans folder is a copy of the scans/label-PX-DSK-4301.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "Read PX-DSK-4301 from the label"
    And the "${sys:desk.reference}" field in the depot app has the value "PX-DSK-4301"

  @windows-ai
  Scenario: A label too far off to read is left to the clerk
    Given the label-text.axxmodel file in the desk-models folder is a copy of the models/label-text-windows.axxmodel file
    And the parcel-on-counter.jpg file in the scans folder is a copy of the scans/parcel-on-counter.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "No reference on the label: type it in"
    And the "${sys:desk.reference}" field in the depot app has the value ""

  @windows-ai
  Scenario: A PC without the label reader leaves the reference to the clerk
    Given the label-text.axxmodel file in the desk-models folder is a copy of the models/no-label-reader-windows.axxmodel file
    And the label-PX-DSK-4301.jpg file in the scans folder is a copy of the scans/label-PX-DSK-4301.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "No label reader on this PC: type the reference in"
