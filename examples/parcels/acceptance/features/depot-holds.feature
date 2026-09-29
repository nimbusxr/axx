Feature: Depot holds

  The depots' systems read parcels, and hold them at the depot until a day the recipient
  chose, through the service's JSON-RPC API. A parcel can be held until it is out for
  delivery. Every call is checked against the API's OpenRPC document, which the service
  answers itself (rpc.discover).

  Background:
    Given the depots jsonrpc service with the following properties:
      | url     | http://localhost:8400/rpc |
      | openrpc | rpc.discover              |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: A depot reads a parcel
    Given a seeds/depot-read.yaml db seed
    When the parcel.get method is called on the depots jsonrpc service with the params:
      """
      ["PX-RPC-7101"]
      """
    Then the depots jsonrpc service's result has the following properties:
      | reference    | PX-RPC-7101 |
      | status       | IN_TRANSIT  |
      | serviceLevel | STANDARD    |
      | heldUntil    | undefined   |

  Scenario: A depot holds a parcel until the day the recipient chose
    Given a seeds/depot-hold.yaml db seed
    When the parcel.hold method is called on the depots jsonrpc service with the following params:
      | reference | PX-RPC-7102          |
      | until     | 2026-10-05           |
      | reason    | recipient on holiday |
    Then the depots jsonrpc service's result has the following properties:
      | reference | PX-RPC-7102 |
      | status    | ON_HOLD     |
      | heldUntil | 2026-10-05  |
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-RPC-7102 |
      | status    | ON_HOLD     |
    And the selection has 1 row

  Scenario: A parcel out for delivery can no longer be held
    Given a seeds/depot-too-late.yaml db seed
    When the parcel.hold method is called on the depots jsonrpc service with the following params:
      | reference | PX-RPC-7103 |
      | until     | 2026-10-05  |
    Then the depots jsonrpc service answered the error 4009 with a message containing 'can no longer be held'
    And the depots jsonrpc service's error has the following properties:
      | data.status | OUT_FOR_DELIVERY |

  Scenario: A depot is told of a parcel the service does not know
    When the parcel.get method is called on the depots jsonrpc service with the following params:
      | reference | PX-RPC-7199 |
    Then the depots jsonrpc service answered the error 4004

  # Some depot scanners leave the day out. That call breaks the API's contract, so the
  # scenario relaxes the check to see what the service answers.
  Scenario: A hold without its day is refused
    Given the OpenRPC validation levels are:
      | validation.params.missing | IGNORE |
    When the parcel.hold method is called on the depots jsonrpc service with the following params:
      | reference | PX-RPC-7104 |
    Then the depots jsonrpc service answered the error -32602 with a message containing 'until'
