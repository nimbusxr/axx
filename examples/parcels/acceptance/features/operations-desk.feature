Feature: The operations desk

  The operations desk looks after parcels from the command line, with `parcels admin` run
  where the service runs: it reprints labels, cancels parcels a shop asks it to, and lists
  a shop's parcels for support. It follows the rules the API and the portal follow.

  Background:
    Given the admin command with the following properties:
      | command | ${sys:parcels.admin} |
      | dir     | ../infra             |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: The desk reprints a parcel's shipping label
    Given a seeds/admin-label.yaml db seed
    When the admin command is run with 'label PX-ADM-6101'
    Then the admin command's exit code is 0
    And the admin command's output is identical to the labels/PX-ADM-6101.zpl file

  Scenario: The desk cannot cancel a parcel the courier has picked up
    Given a seeds/admin-picked-up.yaml db seed
    When the admin command is run with 'cancel PX-ADM-6102'
    Then the admin command's exit code is 3
    And the admin command's error output contains 'PX-ADM-6102 is IN_TRANSIT: it can no longer be cancelled'
    And the admin command's output is empty
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-ADM-6102 |
    And the selection has 1 row

  Scenario: The desk cancels the parcels a shop sends, one reference a line
    Given a seeds/admin-cancel.yaml db seed
    When the admin command is run with 'cancel -' and the input:
      """
      PX-ADM-6103
      PX-ADM-6104
      """
    Then the admin command's exit code is 0
    And the admin command's output is:
      """
      cancelled PX-ADM-6103
      cancelled PX-ADM-6104
      """
    And a selection of rows is retrieved from the parcels.parcels table where:
      | sender | sorrel-and-sage |
    And the selection has 0 rows

  Scenario: The desk lists a shop's parcels, oldest first
    Given a seeds/admin-parcels.yaml db seed
    When the admin command is run with 'parcels --shop pebble-and-pine'
    Then the admin command's exit code is 0
    And the admin command's output has the following properties:
      | shop                    | pebble-and-pine |
      | parcels[0].reference    | PX-ADM-6105     |
      | parcels[0].status       | REGISTERED      |
      | parcels[1].reference    | PX-ADM-6106     |
      | parcels[1].status       | IN_TRANSIT      |
      | parcels[1].serviceLevel | EXPRESS         |
      | parcels[1].weightGrams  | 2400            |
