Feature: Parcels MCP server

  The service serves an MCP server for the AI assistants of shops and recipients: through its
  tools they track parcels and hold them at their depot, and they read parcels' labels and use
  its delivery-update prompt. The scenarios call it as an assistant does, over HTTP and over
  stdio, and check each tool's arguments and results against the tool's own schemas.

  Background:
    Given the parcels mcp server with the following properties:
      | url | http://localhost:8400/mcp |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: An assistant sees what the tools do before it calls them
    Then the parcels mcp server has the track_parcel tool with the following properties:
      | title                    | Track a parcel |
      | annotations.readOnlyHint | true           |
      | inputSchema.required[0]  | reference      |
    And the parcels mcp server has the hold_parcel tool
    And the parcels mcp server does not have the cancel_parcel tool

  Scenario: An assistant tracks a parcel
    Given a seeds/mcp-track.yaml db seed
    When the track_parcel tool is called on the parcels mcp server with the following arguments:
      | reference | PX-MCP-9101 |
    Then the track_parcel tool's result is not an error
    And the track_parcel tool's result has the following properties:
      | status       | OUT_FOR_DELIVERY |
      | serviceLevel | EXPRESS          |
      | city         | Leipzig          |

  Scenario: An assistant holds a parcel at its depot
    Given a seeds/mcp-hold.yaml db seed
    When the hold_parcel tool is called on the parcels mcp server with the following arguments:
      | reference | PX-MCP-9102 |
      | until     | 2026-10-05  |
    Then the hold_parcel tool's result has the following properties:
      | status    | ON_HOLD    |
      | heldUntil | 2026-10-05 |
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-MCP-9102 |
      | status    | ON_HOLD     |
    And the selection has 1 row

  Scenario: A parcel out for delivery can no longer be held
    Given a seeds/mcp-too-late.yaml db seed
    When the hold_parcel tool is called on the parcels mcp server with the following arguments:
      | reference | PX-MCP-9104 |
      | until     | 2026-10-05  |
    Then the hold_parcel tool's result is an error
    And the hold_parcel tool's result contains 'PX-MCP-9104 is out for delivery: it can no longer be held.'

  Scenario: A hold without its day is refused by the server too
    Given a seeds/mcp-no-day.yaml db seed
    And the MCP validation levels are:
      | validation.arguments.schema.required | IGNORE |
    When the hold_parcel tool is called on the parcels mcp server with the following arguments:
      | reference | PX-MCP-9105 |
    Then the hold_parcel tool's result is an error
    And the hold_parcel tool's result contains 'is not a day'

  Scenario: A tool the server does not have is refused
    When the cancel_parcel tool is called on the parcels mcp server with the following arguments:
      | reference | PX-MCP-9101 |
    Then the cancel_parcel tool's call failed with the error code -32602

  Scenario: An assistant reads a label and writes a delivery update
    Given a seeds/mcp-label.yaml db seed
    When the parcels://PX-MCP-9103/label resource is read from the parcels mcp server
    Then the parcels://PX-MCP-9103/label resource has the following properties:
      | reference    | PX-MCP-9103 |
      | serviceLevel | STANDARD    |
    When the delivery_update prompt is requested from the parcels mcp server with the following arguments:
      | reference | PX-MCP-9103 |
    Then the delivery_update prompt contains 'parcel PX-MCP-9103'
    And the delivery_update prompt contains 'Do not mention their address.'

  Scenario: An assistant runs the server as a command, over stdio
    Given a seeds/mcp-stdio.yaml db seed
    And the desk mcp server with the following properties:
      | command | ${sys:parcels.mcp} |
      | dir     | ../infra           |
    When the track_parcel tool is called on the desk mcp server with the following arguments:
      | reference | PX-MCP-9106 |
    Then the track_parcel tool's result on the desk mcp server has the following properties:
      | status | OUT_FOR_DELIVERY |
