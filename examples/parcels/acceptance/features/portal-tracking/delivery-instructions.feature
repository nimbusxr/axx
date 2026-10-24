Feature: A recipient's delivery instructions

  A recipient tells the courier where to leave the parcel, from its tracking page. They write
  in any language, or say it: the page understands them with the browser's own language models
  (Chrome's built-in AI: its language detector, translator and proofreader, and its speech
  recognition), and the service keeps the instructions in English, the couriers' language, as
  the parcel's delivery note, which the couriers' apps show. The browser's models are the
  browser's, so the scenarios give it stubs of their APIs instead (../../models/instructions-*),
  prebuilt, in the web app's axx-models folder.

  Background:
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |
    And the parcels-models folder with the following properties:
      | owner | app:parcels |
      | path  | axx-models  |
    And the language.axxmodel file in the parcels-models folder is a copy of the models/instructions-language.axxmodel file
    And the translation.axxmodel file in the parcels-models folder is a copy of the models/instructions-translation.axxmodel file
    And the spelling.axxmodel file in the parcels-models folder is a copy of the models/instructions-spelling.axxmodel file
    And the speech.axxmodel file in the parcels-models folder is a copy of the models/instructions-speech.axxmodel file

  Scenario: Instructions in German are kept in English for the courier
    Given a seeds/portal-instructions-german.yaml db seed
    And the "/track/PX-WEB-5701" page is opened
    When the "Instructions for your courier" field is filled with "Bitte beim Nachbarn in Nummer 12 abgeben."
    And the "Save instructions" button is clicked
    Then the page shows "Saved. Your courier reads: Please leave it with the neighbour at number 12."
    And a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
      | deliveryNote | Please leave it with the neighbour at number 12. |
    And the selection has 1 row

  Scenario: Instructions in English are kept as the recipient wrote them
    Given a seeds/portal-instructions-english.yaml db seed
    And the "/track/PX-WEB-5702" page is opened
    When the "Instructions for your courier" field is filled with "Leave it in the porch, please."
    And the "Save instructions" button is clicked
    Then the page shows "Saved. Your courier reads: Leave it in the porch, please."
    And a selection of rows is retrieved from the parcels.parcels table where the details jsonb column contains:
      | deliveryNote | Leave it in the porch, please. |
    And the selection has 1 row

  Scenario: The page offers to correct a misspelling
    Given a seeds/portal-instructions-spelling.yaml db seed
    And the "/track/PX-WEB-5703" page is opened
    And the "Instructions for your courier" field is filled with "Pleese ring twice."
    When the "Check spelling" button is clicked
    Then the page shows "Did you mean: Please ring twice."

  Scenario: A recipient takes the page's correction
    Given a seeds/portal-instructions-correction.yaml db seed
    And the "/track/PX-WEB-5705" page is opened
    And the "Instructions for your courier" field is filled with "Pleese ring twice."
    And the "Check spelling" button is clicked
    When the "Use the correction" button is clicked
    Then the "Instructions for your courier" field has the value "Please ring twice."

  Scenario: A recipient says their instructions
    Given a seeds/portal-instructions-dictated.yaml db seed
    And the "/track/PX-WEB-5704" page is opened
    When the "Dictate" button is clicked
    And the parcels app's microphone hears the "recordings/behind-the-gate.wav" file
    Then the "Instructions for your courier" field has the value "Behind the green gate, please."
