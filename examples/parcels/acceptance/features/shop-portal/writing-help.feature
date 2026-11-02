Feature: The portal helps a shop write

  The portal helps a shop write with the browser's own language models (Chrome's built-in AI).
  It fills in a recipient's address from what the customer wrote, with the browser's Prompt
  API, which answers the address's fields as JSON. It drafts a message to a parcel's recipient
  and makes it shorter, with the browser's Writer and Rewriter, and puts a recipient's long
  delivery note in a line, with its Summarizer. What the shop writes stays in its browser. The
  browser's models are the browser's, so the scenarios give it stubs of their APIs instead
  (../../models/portal-*), prebuilt, in the web app's axx-models folder.

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
    And the address.axxmodel file in the parcels-models folder is a copy of the models/portal-address.axxmodel file
    And the draft.axxmodel file in the parcels-models folder is a copy of the models/portal-draft.axxmodel file
    And the shorter.axxmodel file in the parcels-models folder is a copy of the models/portal-shorter.axxmodel file
    And the summary.axxmodel file in the parcels-models folder is a copy of the models/portal-summary.axxmodel file

  Scenario: The portal fills in the address a customer wrote
    Given the "/parcels/new" page is opened
    And the "The customer's address, as they wrote it" field is filled with "Hi! Please send it to Mara Lindqvist, Birkenallee 3, 01067 Dresden. Thanks!"
    When the "Fill in the address" button is clicked
    Then the "Recipient name" field has the value "Mara Lindqvist"
    And the "Street" field has the value "Birkenallee 3"
    And the "Postcode" field has the value '"01067"'
    And the "City" field has the value "Dresden"
    And the "Destination country" field has the value "Germany"

  Scenario: The portal drafts a message to a parcel's recipient
    Given a seeds/portal-writing-message.yaml db seed
    And the "/parcels/PX-WEB-5801" page is opened
    And the "What it should say" field is filled with "It arrives a day late. Sorry."
    When the "Draft the message" button is clicked
    Then the "Message to the recipient" field has the value "Dear Jonas Weber, your parcel arrives a day later than we said: it missed its connection at the depot. We are sorry for the wait. Kind regards, maple-crafts"

  Scenario: The portal makes a message shorter
    Given a seeds/portal-writing-shorter.yaml db seed
    And the "/parcels/PX-WEB-5802" page is opened
    And the "What it should say" field is filled with "It arrives a day late. Sorry."
    And the "Draft the message" button is clicked
    When the "Make it shorter" button is clicked
    Then the "Message to the recipient" field has the value "Your parcel arrives a day late: it missed its connection. Sorry for the wait!"

  Scenario: A recipient's long delivery note is put in a line for the shop
    Given a seeds/portal-writing-note.yaml db seed
    And the "/parcels/PX-WEB-5803" page is opened
    When the "Summarize the note" button is clicked
    Then the page shows "In short: Behind the green gate, in the box by the shed; close the gate (dog); box full: the Meyers at 14."
