Feature: Planning in the shop portal

  Shops plan the day each parcel is picked up, and set how pickups work for them: the
  address, the days, whether they hear of each delivery, and the logo on their labels.

  Background:
    Given the parcels web app with the following properties:
      | url | ${sys:portal.url} |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A shop sees what a pickup holds
    Given a seeds/portal-pickup-summary.yaml db seed
    And the "/parcels?shop=hazel-and-hide" page is opened
    When the "Pickup PX-WEB-5311" element is clicked
    Then the page shows "PX-WEB-5311: 3.1 kg to Rosa Beck"

  Scenario: A shop plans the pickup of a parcel
    Given a seeds/portal-pickups.yaml db seed
    And the "/parcels?shop=linden-leather" page is opened
    When the "Pickup PX-WEB-5302" element is dragged onto the "Friday" element
    Then the page shows "PX-WEB-5302 is picked up on Friday"
    And a selection of rows is retrieved from the parcels.pickups table where:
      | reference | PX-WEB-5302 |
      | day       | Friday      |
    And the selection has 1 row

  Scenario: A shop is told when a pickup cannot be planned for want of a connection
    Given a seeds/portal-pickup-offline.yaml db seed
    And the page's requests to "/pickups" fail
    And the "/parcels?shop=fern-and-flax" page is opened
    When the "Pickup PX-WEB-5321" element is dragged onto the "Tomorrow" element
    Then the page shows "The pickup could not be planned: check your connection and try again"

  Scenario: A shop is told to try again when pickups cannot be planned right now
    Given a seeds/portal-pickup-busy.yaml db seed
    And the page's requests to "/pickups" answer with status 503
    And the "/parcels?shop=brook-and-birch" page is opened
    When the "Pickup PX-WEB-5322" element is dragged onto the "Friday" element
    Then the page shows "Pickups cannot be planned right now: try again later"

  Scenario: A shop's account is not one of its settings
    When the "/settings?shop=ashgrove-goods" page is opened
    Then the "Shop account" field is disabled
    And the "Pickup address" field is enabled

  Scenario: A shop sets its pickup days, and the logo on its labels
    Given the "/settings?shop=willow-and-wax" page is opened
    When the "Pickup address" field is filled with "Weserstr. 40, 12045 Berlin"
    And the following options are chosen in the "Pickup days" field:
      | Monday   |
      | Thursday |
    And the "Email me when a parcel is delivered" checkbox is checked
    And the logos/willow-and-wax.png file is uploaded in the "Upload your logo" field
    And the "Save the settings" button is clicked
    Then the page shows "Settings saved"
    And the "Email me when a parcel is delivered" checkbox is ticked
    And the "Pickup address" field has the value "Weserstr. 40, 12045 Berlin"
    And the page shows "Logo: willow-and-wax.png"
    And a selection of rows is retrieved from the parcels.shop_settings table where:
      | shop        | willow-and-wax     |
      | pickup_days | Monday,Thursday    |
      | logo        | willow-and-wax.png |
    And the selection has 1 row
