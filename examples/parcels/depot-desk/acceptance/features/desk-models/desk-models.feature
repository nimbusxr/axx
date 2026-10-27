@desktop @macos-models
Feature: The depot desk's models, on a Mac

  The desk's macOS builds use the Mac's own models (../../../README.md): Apple's Vision reads a parcel's reference off the scan the depot's label camera saves, and checks the
  scan shows a parcel before the desk keeps it with the arrival, the people in it blurred out;
  Apple's speech recognizer takes the clerk's dictated handover note; and Apple's language model
  sums up the day for the next shift, on the Mac or on Apple's servers. Each scenario puts stubs
  of those models (models/*-macos) in the desk's axx-models folder, and the label camera's scans
  in its Scans folder: no scenario asks a real model.

  Background:
    Given the depot macos app with the following properties:
      | app         | ${sys:desk.app}         |
      | args        | ${sys:desk.args}        |
      | preferences | ${sys:desk.preferences} |
    And the desk folder with the following properties:
      | owner | app:depot        |
      | path  | ${sys:desk.data} |
    And the scans folder with the following properties:
      | owner | app:depot              |
      | path  | ${sys:desk.data}/Scans |
    And the desk-models folder with the following properties:
      | owner | app:depot  |
      | path  | axx-models |
    And the label-text.axxmodel file in the desk-models folder is a copy of the models/label-text-macos.axxmodel file
    And the photo-check.axxmodel file in the desk-models folder is a copy of the models/photo-check-macos.axxmodel file
    And the photo-people.axxmodel file in the desk-models folder is a copy of the models/photo-people-macos.axxmodel file
    And the photo-people-mask.axxmodel file in the desk-models folder is a copy of the models/photo-people-mask-macos.axxmodel file

  Scenario: A clerk reads a parcel's reference off its label
    Given the label-PX-DSK-4301.jpg file in the scans folder is a copy of the scans/label-PX-DSK-4301.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "Read PX-DSK-4301 from the label"
    And the "${sys:desk.reference}" field in the depot app has the value "PX-DSK-4301"

  Scenario: A label too far off to read is left to the clerk
    Given the parcel-on-counter.jpg file in the scans folder is a copy of the scans/parcel-on-counter.jpg file
    And the depot app is launched
    When the "Depot" element is clicked in the depot app
    And the "Read label" element is clicked in the depot app
    Then the depot app shows "No reference on the label: type it in"
    And the "${sys:desk.reference}" field in the depot app has the value ""

  Scenario: A parcel's scan is kept with its arrival
    Given the parcel-on-counter.jpg file in the scans folder is a copy of the scans/parcel-on-counter.jpg file
    And the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4303"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4303: Standard, photo kept"
    And the Photos/PX-DSK-4303.jpg file in the desk folder is identical to the scans/parcel-on-counter.jpg file

  Scenario: A scan of something else than a parcel is not kept
    Given the delivery-papers.jpg file in the scans folder is a copy of the scans/delivery-papers.jpg file
    And the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4304"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4304: Standard, photo not kept: it shows no parcel"

  Scenario: The people in a scan are blurred out of the photo the desk keeps
    Given the parcel-and-clerk.jpg file in the scans folder is a copy of the scans/parcel-and-clerk.jpg file
    And the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4305"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4305: Standard, photo kept, people blurred out"

  Scenario: A clerk dictates a note for the handover
    Given the handover-note.axxmodel file in the desk-models folder is a copy of the models/handover-note-macos.axxmodel file
    And the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the "Dictate note" button is clicked in the depot app
    And the depot app's microphone hears the "recordings/night-porter.wav" file
    Then the depot app shows "Note: Hand the fragile parcels to the night porter."

  Scenario: The next shift gets the day's summary
    Given the day-summary.axxmodel file in the desk-models folder is a copy of the models/day-summary-macos.axxmodel file
    And the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4311"
    And the "Fragile" checkbox is clicked in the depot app
    And the "Express" element is clicked in the depot app
    And the Enter key is pressed in the depot app
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4312"
    And the "Standard" element is clicked in the depot app
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Day summary" element is clicked in the depot app
    Then the depot app shows "Day summary: 2 parcels today. Mind PX-DSK-4311: express and fragile."

  Scenario: A Mac without Apple Intelligence asks Apple's servers for the summary
    Given the day-summary.axxmodel file in the desk-models folder is a copy of the models/no-apple-intelligence-macos.axxmodel file
    And the day-summary-server.axxmodel file in the desk-models folder is a copy of the models/day-summary-server-macos.axxmodel file
    And the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4311"
    And the "Fragile" checkbox is clicked in the depot app
    And the "Express" element is clicked in the depot app
    And the Enter key is pressed in the depot app
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4312"
    And the "Standard" element is clicked in the depot app
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Day summary" element is clicked in the depot app
    Then the depot app shows "Day summary from Apple's servers: 2 parcels today. Mind PX-DSK-4311: express and fragile."

  Scenario: Without a model, the desk says why there is no summary
    Given the day-summary.axxmodel file in the desk-models folder is a copy of the models/no-apple-intelligence-macos.axxmodel file
    And the day-summary-server.axxmodel file in the desk-models folder is a copy of the models/no-server-model-macos.axxmodel file
    And the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4313"
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Day summary" element is clicked in the depot app
    Then the depot app shows "No day summary: this Mac has no Apple Intelligence"
