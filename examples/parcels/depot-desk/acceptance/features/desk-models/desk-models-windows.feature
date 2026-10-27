@desktop @windows-models
Feature: The depot desk's models, on Windows

  The desk's Windows Forms, WPF and WinUI builds read the handover rules aloud with SAPI, in the
  user's voice (../../../README.md). Each scenario puts a stub of a voice (a Piper voice,
  models/handover-voice) in the desk's axx-models folder, which the models-windows pack makes the
  user's voice: no scenario asks a real voice.

  Background:
    Given the depot windows app with the following properties:
      | app      | ${sys:desk.app}      |
      | args     | ${sys:desk.args}     |
      | registry | ${sys:desk.registry} |
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
