Feature: Shop portal

  Shops that don't connect their own systems use the portal: they get a price, register
  their parcels in a form and follow them. A parcel registered in the portal is checked,
  stored and announced like any other.

  Background:
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The quote form is ready for a parcel within Germany
    When the "/quote" page is opened
    Then the page title is "Get a quote · Parcels"
    And the "Weight (grams)" field has the focus
    And the "Standard" option is selected

  Scenario: A shop gets a price for a parcel within Germany
    Given the "/quote" page is opened
    When the "Weight (grams)" field is filled with "1200"
    And "Germany" is chosen in the "Destination country" field
    And the "Postcode" field is filled with "10115"
    And the "Get a quote" button is clicked
    Then the page shows "Price: 6.90 EUR · delivered in 2 days"
    And the browser sent a POST request to "/quote"
    And the "testid=quote" element shows "6.90 EUR"
    And the "Weight (grams)" field has the value "1200"
    And the "Destination country" field has the value "Germany"
    And the page has no script errors

  Scenario: The portal says what Express means
    Given the "/quote" page of the parcels web app is opened
    When the pointer is moved over "About Express"
    Then the page shows "Delivered the next working day within Germany"

  Scenario: Express abroad costs more and arrives within two days
    Given the "/quote" page is opened
    When the "Weight (grams)" field is filled with "1200"
    And "France" is chosen in the "Destination country" field
    And the "Postcode" field is filled with "75011"
    And the "Express" option is chosen
    And the "Get a quote" button is clicked
    Then the page shows "Price: 19.90 EUR · delivered in 2 days"
    And the "Standard" option is not selected

  Scenario: Shops can get a quote whatever their abilities
    When the "/quote" page is opened
    Then the page has no accessibility violations

  Scenario: The registration form says what each field is
    When the "/parcels/new" page is opened
    Then the "css=form" element's accessible structure is:
      """
      - paragraph:
        - textbox "Shop account"
      - paragraph:
        - textbox "Parcel reference"
      - group "Service":
        - radio "Standard" [checked]
        - radio "Express"
      - group "Delivery":
        - paragraph:
          - checkbox "Leave with a neighbour" [checked]
      - button "Register the parcel" [disabled]
      """
    And the "css=form" element has no accessibility violations

  Scenario: The quote page is quick and sound on a phone
    When the "/quote" page is opened
    Then the "/quote" page scores at least:
      | performance    | 90  |
      | accessibility  | 100 |
      | best practices | 100 |
      | seo            | 100 |
    And the "/quote" page loads within:
      | largest contentful paint | 2.5s  |
      | total blocking time      | 200ms |
      | cumulative layout shift  | 0.1   |

  Scenario: The quote page looks as designed
    Given the desktop web app with the following properties:
      | url      | ${sys:portal.url} |
      | viewport | 1280x800          |
    And the browser's clock is set to "2026-09-25T09:00:00Z"
    When the "/quote" page of the desktop web app is opened
    Then the page looks like the "quote" screenshot

  Scenario: The portal follows the shop's dark mode
    Given the dark web app with the following properties:
      | url          | ${sys:portal.url} |
      | viewport     | 1280x800          |
      | color scheme | dark              |
    And the browser's clock is set to "2026-09-25T09:00:00Z"
    When the "/quote" page of the dark web app is opened
    Then the page looks like the "quote dark" screenshot

  Scenario: An Express quote looks as designed
    Given the "/quote" page is opened
    When the "Weight (grams)" field is filled with "1200"
    And "France" is chosen in the "Destination country" field
    And the "Postcode" field is filled with "75011"
    And the "Express" option is chosen
    And the "Get a quote" button is clicked
    Then the "testid=quote" element looks like the "express quote" screenshot

  Scenario: A new parcel waits for its shop and its reference
    When the "/parcels/new" page is opened
    Then the "Register the parcel" button is disabled
    And the "Leave with a neighbour" checkbox is ticked

  Scenario: A parcel can be registered once its shop and its reference are there
    Given the "/parcels/new" page is opened
    When the "Shop account" field is filled with "fjord-outdoor"
    And the "Parcel reference" field is filled with "PX-WEB-5000"
    Then the "Register the parcel" button is enabled

  Scenario: A shop keeps a parcel from being left with a neighbour
    Given the "/parcels/new" page is opened
    When the "Leave with a neighbour" checkbox is unchecked
    Then the "Leave with a neighbour" checkbox is not ticked

  Scenario: A parcel registered in the portal is stored and announced
    Given the events kafka service with the following properties:
      | brokers | ${sys:local.host}:9092 |
    And a parcel-events kafka topic client with the following properties:
      | consumer.value.deserializer  | io.confluent.kafka.serializers.KafkaAvroDeserializer |
      | consumer.schema.registry.url | http://${sys:local.host}:9081                        |
    And the "/parcels/new" page is opened
    When the "Shop account" field is filled with "fjord-outdoor"
    And the "Parcel reference" field is filled with "PX-WEB-5001"
    And the "Weight (grams)" field is filled with "1200"
    And the "Express" option is chosen
    And the "Recipient name" field is filled with "Anna Weber"
    And the "Street" field is filled with "Invalidenstr. 116"
    And the "Postcode" field is filled with "10115"
    And the "City" field is filled with "Berlin"
    And "Germany" is chosen in the "Destination country" field
    And the "Signature on delivery" checkbox is checked
    And the "Leave with a neighbour" checkbox is unchecked
    And the "Register the parcel" button is clicked
    Then the "/parcels/PX-WEB-5001" page is shown
    And the page shows "Parcel PX-WEB-5001 registered"
    And the page shows "Signature on delivery: yes"
    And the page shows "Leave with a neighbour: no"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-WEB-5001 |
    And the selection has 1 row
    And the 1st row details property for the selection json properties are:
      | source             | portal |
      | signature          | true   |
      | leaveWithNeighbour | false  |
    And the parcel-events kafka event named registered key is PX-WEB-5001
    And the parcel-events kafka event named registered payload properties are:
      | $.reference    | PX-WEB-5001 |
      | $.serviceLevel | EXPRESS     |
      | $.source       | portal      |

  Scenario: A parcel abroad goes with its customs invoice
    Given the "/parcels/new" page is opened
    When the "Shop account" field is filled with "maple-crafts"
    And the "Parcel reference" field is filled with "PX-WEB-5002"
    And the "Weight (grams)" field is filled with "800"
    And the "Recipient name" field is filled with "Marc Petit"
    And the "Street" field is filled with "Rue Oberkampf 40"
    And the "Postcode" field is filled with "75011"
    And the "City" field is filled with "Paris"
    And "France" is chosen in the "Destination country" field
    And the invoices/INV-2041.pdf file is uploaded in the "Customs invoice" field
    And the "Register the parcel" button is clicked
    Then the page shows "Parcel PX-WEB-5002 registered"
    And the page shows "Customs invoice: INV-2041.pdf"

  Scenario: The portal turns down an address it cannot deliver to
    Given the "/parcels/new" page is opened
    When the "Shop account" field is filled with "fjord-outdoor"
    And the "Parcel reference" field is filled with "PX-WEB-5003"
    And the "Weight (grams)" field is filled with "1200"
    And the "Recipient name" field is filled with "Jonas Brandt"
    And the "Postcode" field is filled with "99901"
    And "Germany" is chosen in the "Destination country" field
    And the "Register the parcel" button is clicked
    Then the page shows "We cannot deliver to that address: no delivery to this postcode"
    And the page does not show "Parcel PX-WEB-5003 registered"
    And the "Parcel reference" field has the value "PX-WEB-5003"
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-WEB-5003 |
    And the selection has 0 rows

  Scenario: A shop sees its parcels
    Given a seeds/portal-shop.yaml db seed
    When the "/parcels?shop=birch-and-bloom" page is opened
    Then the page shows a table row where:
      | Parcel    | PX-WEB-5101   |
      | Recipient | Lena Hoffmann |
      | Status    | IN_TRANSIT    |
    And the page shows a table row where:
      | Parcel | PX-WEB-5102 |
      | Status | REGISTERED  |

  Scenario: A shop opens a parcel from its list
    Given a seeds/portal-open.yaml db seed
    And the "/parcels?shop=fern-and-flint" page is opened
    When the "PX-WEB-5141" link is clicked
    Then the "/parcels/PX-WEB-5141" page is shown
    And the page shows "Signature on delivery: yes"

  Scenario: The browser's back button goes back to the page before
    Given the "/quote" page is opened
    And the "/parcels/new" page is opened
    When the browser's back button is clicked
    Then the "/quote" page is shown

  Scenario: A shop finds a parcel by its reference
    Given a seeds/portal-find.yaml db seed
    And the "/parcels?shop=lark-and-loom" page is opened
    When the "Find a parcel" field is filled with "PX-WEB-5151"
    And the Enter key is pressed in the "Find a parcel" field
    Then the "/parcels/PX-WEB-5151" page is shown

  Scenario: A parcel's page leads back to the shop's parcels
    Given a seeds/portal-return.yaml db seed
    And the "/parcels/PX-WEB-5161" page is opened
    When the "Your parcels" link is clicked
    Then the "/parcels" page is shown

  Scenario: A shop exports its parcels
    Given a seeds/portal-export.yaml db seed
    And the "/parcels?shop=juniper-supply" page is opened
    When the "Export as CSV" link is clicked
    Then the "parcels-juniper-supply.csv" file is downloaded
    And the downloaded "parcels-juniper-supply.csv" file contains "PX-WEB-5171,Paul Wagner,EXPRESS,IN_TRANSIT"

  Scenario: On a phone, the portal keeps its links in a menu
    Given the phone web app with the following properties:
      | url    | ${sys:portal.url} |
      | engine | webkit            |
      | device | iPhone 15         |
    When the "/quote" page of the phone web app is opened
    Then the "Menu" button is shown
    And the "Register a parcel" link is not shown

  Scenario: Shops register parcels from their phones too
    Given the phone web app with the following properties:
      | url    | ${sys:portal.url} |
      | engine | webkit            |
      | device | iPhone 15         |
    And the "/quote" page of the phone web app is opened
    When the "Menu" button is tapped
    And the "Register a parcel" link is tapped
    Then the "/parcels/new" page is shown
    And the "Register the parcel" button is disabled

  Scenario: Parcels registered before 14:00 are picked up the same day
    Given the berlin web app with the following properties:
      | url      | ${sys:portal.url} |
      | timezone | Europe/Berlin     |
    And the browser's clock is set to "2026-09-25T10:00:00+02:00"
    When the "/quote" page of the berlin web app is opened
    Then the page shows "Registered before 14:00, a parcel is picked up today"

  Scenario: Parcels registered after 14:00 are picked up the next day
    Given the berlin web app with the following properties:
      | url      | ${sys:portal.url} |
      | timezone | Europe/Berlin     |
    And the browser's clock is set to "2026-09-25T10:00:00+02:00"
    And the "/quote" page of the berlin web app is opened
    When the browser's clock is set to "2026-09-25T15:30:00+02:00"
    And the page is reloaded
    Then the page shows "A parcel registered now is picked up tomorrow"

  Scenario: The portal ends a session after 30 minutes without activity
    Given the browser's clock is set to "2026-09-25T09:00:00+02:00"
    And the "/quote" page is opened
    When the browser's clock is moved forward by 31m
    Then the page shows "Your session ended after 30 minutes without activity"

  Scenario: The portal says when it cannot save anything
    Given the "/quote" page is opened
    When the browser is offline
    Then the page shows "You are offline: nothing is saved until you are back online"

  Scenario: The portal says when it can save again
    Given the "/quote" page is opened
    And the browser is offline
    And the page shows "You are offline"
    When the browser is online
    Then the page does not show "You are offline"

  Scenario: A returning shop sees its first parcels
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url} |
      | cookie.shop | harbour-hardware  |
    And a seeds/portal-busy.yaml db seed
    When the "/parcels" page of the shop web app is opened
    Then the "testid=parcel-count" element shows "25 parcels"
    And the page shows 20 "testid=parcel-row" elements
    And the page shows 3 table rows where:
      | Status | IN_TRANSIT |

  Scenario: More parcels show as a shop scrolls down its list
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url} |
      | cookie.shop | kiln-and-copper   |
    And a seeds/portal-scroll.yaml db seed
    And the "/parcels" page of the shop web app is opened
    When the page is scrolled to the bottom
    Then the page shows 25 "testid=parcel-row" elements
    And the "xpath=//tbody/tr[25]/td[1]" element shows "PX-WEB-5425"
    And the "Back to the top" button is shown

  Scenario: At the top of its list, a shop needs no way back up
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url} |
      | cookie.shop | ashgrove-goods    |
    And a seeds/portal-top.yaml db seed
    And the "/parcels" page of the shop web app is opened
    And the page is scrolled to the bottom
    And the "Back to the top" button is shown
    When the page is scrolled to the top
    Then the "Back to the top" button is not shown

  Scenario: A shop opens a parcel with a double click on its row
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url} |
      | cookie.shop | saltmarsh-supply  |
    And a seeds/portal-double-click.yaml db seed
    And the "/parcels" page of the shop web app is opened
    When the "Ella Brandt" element is double-clicked
    Then the "/parcels/PX-WEB-5501" page is shown

  Scenario: A shop tracks a parcel from its row's menu
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url}  |
      | cookie.shop | thistle-and-thread |
    And a seeds/portal-row-menu.yaml db seed
    And the "/parcels" page of the shop web app is opened
    When the "Nora Fischer" element is right-clicked
    And the "Track this parcel" menu item is clicked
    Then the "/track/PX-WEB-5511" page is shown
    And the page shows "Your parcel from thistle-and-thread is registered."

  Scenario: A shop finds its pickups below its parcels
    Given the shop web app with the following properties:
      | url         | ${sys:portal.url} |
      | cookie.shop | quarry-and-quill  |
    And a seeds/portal-pickup-list.yaml db seed
    And the "/parcels" page of the shop web app is opened
    When the "Pickups" element is scrolled into view
    Then the "Pickup PX-WEB-5521" element is shown
