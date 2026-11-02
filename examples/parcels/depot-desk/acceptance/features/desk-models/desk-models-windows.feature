@desktop @windows-models
Feature: The depot desk's models, on Windows

  The desk's Windows builds use the models Windows has (../../../README.md): the Windows Forms, WPF
  and WinUI builds read the handover rules aloud with SAPI, in the user's voice, and the WinUI
  build uses the Windows AI APIs (@windows-ai): TextRecognizer reads a parcel's reference off the
  label camera's scan, ImageDescriptionGenerator says whether a scan shows a parcel, which the desk
  keeps with its arrival, and the language model, Phi Silica, sums up the day for the next shift.
  Each scenario puts stubs of those models (a Piper voice, models/handover-voice; stubs of the
  APIs, models/*-windows) in the desk's axx-models folder, which the models-windows pack answers
  the desk's calls from, and the label camera's scans in its Scans folder: no scenario asks a real
  model.

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
    And the desk folder with the following properties:
      | owner | app:depot        |
      | path  | ${sys:desk.data} |

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
    And the "${sys:desk.reference}" field in the depot app has the value '""'

  @windows-ai
  Scenario: A PC without the label reader leaves the reference to the clerk
    Given the label-text.axxmodel file in the desk-models folder is a copy of the models/no-label-reader-windows.axxmodel file
    And the label-PX-DSK-4301.jpg file in the scans folder is a copy of the scans/label-PX-DSK-4301.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "No label reader on this PC: type the reference in"

  @windows-ai
  Scenario: A parcel's scan is kept with its arrival
    Given the photo-check.axxmodel file in the desk-models folder is a copy of the models/photo-check-windows.axxmodel file
    And the parcel-on-counter.jpg file in the scans folder is a copy of the scans/parcel-on-counter.jpg file
    And the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4303"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4303: Standard, photo kept"
    And the Photos/PX-DSK-4303.jpg file in the desk folder is identical to the scans/parcel-on-counter.jpg file

  @windows-ai
  Scenario: A scan that shows no parcel is not kept
    Given the photo-check.axxmodel file in the desk-models folder is a copy of the models/photo-check-windows.axxmodel file
    And the delivery-papers.jpg file in the scans folder is a copy of the scans/delivery-papers.jpg file
    And the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4304"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4304: Standard, photo not kept: it shows no parcel"

  @windows-ai
  Scenario: The next shift gets the day's summary
    Given the day-summary.axxmodel file in the desk-models folder is a copy of the models/day-summary-windows.axxmodel file
    And the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4311"
    And the "Fragile" checkbox is clicked in the depot app
    And the "Express" element is clicked in the depot app
    And the "Register" button is clicked in the depot app
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4312"
    And the "Standard" element is clicked in the depot app
    And the "Register" button is clicked in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Day summary" element is clicked in the depot app
    Then the depot app shows "Day summary: 2 parcels today. Mind PX-DSK-4311: express and fragile."

  @windows-ai
  Scenario: A PC without the language model says why there is no summary
    Given the day-summary.axxmodel file in the desk-models folder is a copy of the models/no-language-model-windows.axxmodel file
    And the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4313"
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Day summary" element is clicked in the depot app
    Then the depot app shows "No day summary: this PC has no language model"
