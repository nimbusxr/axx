Feature: Parcels in the shop portal

  A parcel's page has what a shop does once the parcel is registered: print its label,
  share its tracking page with the recipient, report a problem, or cancel the parcel
  until it is picked up.

  Background:
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A parcel's page shows where the parcel goes, and where to follow it
    Given a seeds/portal-address.yaml db seed
    When the "/parcels/PX-WEB-5115" page is opened
    Then the page shows "Ship to: Sofia Richter, Goethestr. 12, 99423 Weimar"
    And the "Track this parcel" link has the href attribute "/portal/track/PX-WEB-5115"

  Scenario: A parcel's page prints without the portal's menus
    Given a seeds/portal-print.yaml db seed
    And the printed web app with the following properties:
      | url   | ${sys:portal.url} |
      | media | print             |
    When the "/parcels/PX-WEB-5191" page of the printed web app is opened
    Then the "Actions" button is not shown
    And the "Get a quote" link is not shown
    And the "History" tab is not shown
    And the page shows "Signature on delivery: yes"

  Scenario: A shop prints a parcel's label
    Given a seeds/portal-label.yaml db seed
    And the "/parcels/PX-WEB-5111" page is opened
    When the "Actions" button is clicked
    And the "Download the label" menu item is clicked
    Then the "PX-WEB-5111-label.zpl" file is downloaded
    And the downloaded "PX-WEB-5111-label.zpl" file is identical to the labels/PX-WEB-5111.zpl file

  Scenario: A parcel on its way can no longer be cancelled
    Given a seeds/portal-track.yaml db seed
    And the "/parcels/PX-WEB-5112" page is opened
    When the "Actions" button is clicked
    Then the "Download the label" menu item is shown
    And the "Cancel the parcel" menu item is not shown

  Scenario: A shop sees where a parcel has been
    Given a seeds/portal-history.yaml db seed
    And the "/parcels/PX-WEB-5116" page is opened
    When the "History" tab is clicked
    Then the page shows a table row where:
      | What | IN_TRANSIT |

  Scenario: A shop shares a parcel's tracking page with its recipient
    Given a seeds/portal-share.yaml db seed
    And the "/parcels/PX-WEB-5117" page is opened
    When the "Track this parcel" link is clicked
    Then the "/track/PX-WEB-5117" page is shown
    And the page shows "Your parcel from maple-crafts is in transit."

  Scenario: The tracking page opens in a browser tab of its own
    Given a seeds/portal-share-closed.yaml db seed
    And the "/parcels/PX-WEB-5118" page is opened
    And the "Track this parcel" link is clicked
    When the browser tab is closed
    Then the "/parcels/PX-WEB-5118" page is shown

  Scenario: The actions menu says it is open
    Given a seeds/portal-actions.yaml db seed
    And the "/parcels/PX-WEB-5123" page is opened
    When the "Actions" button is clicked
    Then the "Actions" button has the aria-expanded attribute "true"
    And the "Cancel the parcel" menu item is shown

  Scenario: The portal asks before it cancels a parcel
    Given a seeds/portal-ask.yaml db seed
    And the "/parcels/PX-WEB-5124" page is opened
    And the "Actions" button is clicked
    When the "Cancel the parcel" menu item is clicked
    Then the dialog shows "Cancel parcel PX-WEB-5124?"

  Scenario: A shop cancels a parcel before it is picked up
    Given a seeds/portal-cancel.yaml db seed
    And the "/parcels/PX-WEB-5121" page is opened
    And the "Actions" button is clicked
    And the "Cancel the parcel" menu item is clicked
    When the dialog is accepted
    Then the page shows "Parcel PX-WEB-5121 cancelled"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-WEB-5121 |
    And the selection has 0 rows

  Scenario: A shop that changes its mind keeps the parcel
    Given a seeds/portal-problem.yaml db seed
    And the "/parcels/PX-WEB-5122" page is opened
    And the "Actions" button is clicked
    And the "Cancel the parcel" menu item is clicked
    When the dialog is dismissed
    And the page is reloaded
    Then the page shows "Parcel PX-WEB-5122 registered"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-WEB-5122 |
    And the selection has 1 row

  Scenario: A shop reports a problem with a parcel
    Given a seeds/portal-report.yaml db seed
    And the "/parcels/PX-WEB-5125" page is opened
    And the "Actions" button is clicked
    And the "Report a problem" menu item is clicked
    When the dialog is answered with "The recipient moved to Kastanienallee 14"
    Then the page shows "Problem reported: The recipient moved to Kastanienallee 14."

  Scenario: A parcel's page looks as designed, whatever the time it was registered
    Given the desktop web app with the following properties:
      | url      | ${sys:portal.url} |
      | viewport | 1280x800          |
    And a seeds/portal-look.yaml db seed
    When the "/parcels/PX-WEB-5181" page of the desktop web app is opened
    Then the page looks like the "parcel" screenshot, apart from:
      | css=time |

  Scenario: A shop in Germany sees times in its own time zone
    Given the berlin web app with the following properties:
      | url      | ${sys:portal.url} |
      | locale   | de-DE             |
      | timezone | Europe/Berlin     |
    And a seeds/portal-times.yaml db seed
    When the "/parcels/PX-WEB-5131" page of the berlin web app is opened
    Then the page shows "Registered: 25.09.2026, 14:03"
