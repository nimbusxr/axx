@desktop
Feature: The depot desk

  The desk at the Leipzig depot (../README.md): a clerk registers the parcels arriving, finds
  them on the day's list of expected parcels, has the courier sign for the handover, and
  closes the day. The feature runs against every build of the desk, each drawn with its
  toolkit's own widgets: where toolkits draw a control differently (Print label is a switch
  or a checkbox, Handover a tab or a text), the steps name it an element. Each scenario starts
  the desk clean: no arrivals, no settings.

  Background:
    Given the depot macos app with the following properties:
      | app         | ${sys:desk.app}         |
      | args        | ${sys:desk.args}        |
      | preferences | ${sys:desk.preferences} |
    And the depot windows app with the following properties:
      | app      | ${sys:desk.app}      |
      | args     | ${sys:desk.args}     |
      | registry | ${sys:desk.registry} |
    And the depot linux app with the following properties:
      | app  | ${sys:desk.app}  |
      | args | ${sys:desk.args} |
    And the desk folder with the following properties:
      | owner | app:depot        |
      | path  | ${sys:desk.data} |

  Scenario: The desk opens with nothing registered
    When the depot app is launched
    Then the depot app shows "No parcels registered yet"
    And the "Leipzig depot" image is shown in the depot app

  @enabled-state
  Scenario: Register waits for a reference
    When the depot app is launched
    Then the "Register" button is disabled in the depot app

  @enabled-state
  Scenario: A reference enables Register
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4201"
    Then the "Register" button is enabled in the depot app

  @field-values
  Scenario: Escape empties the reference
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4201"
    And the Escape key is pressed in the depot app
    Then the "${sys:desk.reference}" field in the depot app has the value ""

  Scenario: A clerk registers a fragile express parcel with a label
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4201"
    And the "Fragile" checkbox is clicked in the depot app
    And the "Express" element is clicked in the depot app
    And the "Print label" element is clicked in the depot app
    And the "Register" button is clicked in the depot app
    Then the depot app shows "Registered PX-DSK-4201: Express, fragile, label printed"

  Scenario: Enter registers a parcel
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4202"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4202: Standard"

  Scenario: A parcel is registered once a day
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4203"
    And the Enter key is pressed in the depot app
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4203"
    And the Enter key is pressed in the depot app
    Then the depot app shows "PX-DSK-4203 is already registered"

  Scenario: An arrival shows how it was registered
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4204"
    And the "Fragile" checkbox is clicked in the depot app
    And the "Express" element is clicked in the depot app
    And the "Register" button is clicked in the depot app
    When the "PX-DSK-4204" element is clicked in the depot app
    Then the depot app shows "PX-DSK-4204: Express, fragile"

  Scenario: A clerk registers a parcel from the day's list
    Given the depot app is launched
    When the "PX-DSK-4138" element is scrolled into view in the depot app
    And the "PX-DSK-4138" element is clicked in the depot app
    And the "Register" button is clicked in the depot app
    Then the depot app shows "Registered PX-DSK-4138: Standard"

  Scenario: The desk keeps the day's arrivals in its data folder
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4205"
    And the Enter key is pressed in the depot app
    Then within 5s the "arrivals.json" file in the desk folder contains "PX-DSK-4205"

  Scenario: A restart keeps the day's arrivals
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4206"
    And the Enter key is pressed in the depot app
    When the depot app is restarted
    Then the depot app shows "1 parcel registered today"
    And the "PX-DSK-4206" element is shown in the depot app

  Scenario: A restart keeps the last service level
    Given the depot app is launched
    And the "Express" element is clicked in the depot app
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4207"
    And the Enter key is pressed in the depot app
    And the depot app is restarted
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4208"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4208: Express"

  Scenario: Every scenario starts the desk clean
    # Other scenarios registered parcels, and Express last: none of it is left.
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4209"
    And the Enter key is pressed in the depot app
    Then the depot app shows "Registered PX-DSK-4209: Standard"

  @menu-items @menu-state
  Scenario: Close day waits for a parcel
    Given the depot app is launched
    When the "Depot" element is clicked in the depot app
    Then within 2s the "Close day" element is disabled in the depot app

  @menu-items @menu-state
  Scenario: A registered parcel can be handed over
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4210"
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    Then within 2s the "Close day" element is enabled in the depot app

  @menu-items
  Scenario: Closing the day hands the parcels over
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4211"
    And the Enter key is pressed in the depot app
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4212"
    And the Enter key is pressed in the depot app
    When the "Depot" element is clicked in the depot app
    And the "Close day" element is clicked in the depot app
    Then the depot app shows "Day closed: 2 parcels handed over"

  Scenario: The handover starts unsigned
    Given the depot app is launched
    When the "Handover" element is clicked in the depot app
    Then the depot app shows "Not signed"

  @pointer-places
  Scenario: A courier signs with a tap
    Given the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the "Courier signature" element in the depot app is clicked at 40, 40
    Then the depot app shows "Signed"

  @pointer-places
  Scenario: A courier signs with a stroke
    Given the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the pointer is dragged from 40, 30 to 140, 30 on the "Courier signature" element in the depot app
    Then the depot app shows "Signed"

  @pointer-places
  Scenario: A courier signs across the middle of the pad
    Given the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the pointer is dragged from -60, 10 to 60, -10 from the middle of the "Courier signature" element in the depot app
    Then the depot app shows "Signed"

  @pointer-places
  Scenario: A signature can be cleared
    Given the depot app is launched
    And the "Handover" element is clicked in the depot app
    And the "Courier signature" element in the depot app is clicked at -40, -30 from its bottom right
    When the "Clear signature" button is clicked in the depot app
    Then the depot app shows "Not signed"

  Scenario: The handover rules are a click away
    Given the depot app is launched
    And the "Handover" element is clicked in the depot app
    When the "Handover rules" element is clicked in the depot app
    Then the depot app shows "Parcels are handed over to the courier at 18:00."

  Scenario: A shortcut leaves no key held for the next click
    # Control held after Control+A would make the click a right click on macOS.
    Given the depot app is launched
    And the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4213"
    When the Control+A key is pressed in the depot app
    And the "Fragile" checkbox is clicked in the depot app
    And the "Register" button is clicked in the depot app
    Then the depot app shows "Registered PX-DSK-4213: Standard, fragile"

  @screenshots
  Scenario: The desk looks as it did when it opens
    When the depot app is launched
    Then the depot app shows "No parcels registered yet"
    And within 20s the depot app looks like the "opened" screenshot

  @screenshots
  Scenario: The desk looks as it did with a reference typed
    # The field has the focus: its blinking cursor is left out of the screenshot.
    Given the depot app is launched
    When the "${sys:desk.reference}" field in the depot app is filled with "PX-DSK-4214"
    Then within 20s the depot app looks like the "typed" screenshot
