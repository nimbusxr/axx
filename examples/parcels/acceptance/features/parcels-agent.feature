Feature: Parcels agent

  Other agents, a shop's or a marketplace's, ask the service's A2A agent about parcels: where one
  is, and to hold one at its depot until a day. It has no model inside: it reads a parcel's
  reference and what it is asked, so its answers are the same every time. Parcels beyond the EU
  are delivered by the partner carrier, whose own agent the parcels agent asks; WireMock mocks it
  from the partner's card (../infra/partner-carrier).

  Background:
    Given the parcels a2a agent with the following properties:
      | url | http://localhost:8400 |
    And a parcels-db database with the following properties:
      | url      | postgres://${sys:local.host}:5432/parcels |
      | user     | parcels                                   |
      | password | parcels                                   |

  Scenario: Another agent reads what the parcels agent does
    Then the parcels a2a agent's card has the following properties:
      | name                   | Parcels agent |
      | capabilities.streaming | true          |
    And the parcels a2a agent has the track-parcel skill
    And the parcels a2a agent has the hold-parcel skill

  Scenario: Where a parcel is
    Given a seeds/a2a-track.yaml db seed
    When a message is sent to the parcels a2a agent:
      """
      Where is PX-A2A-9201?
      """
    Then the parcels a2a agent's task is completed
    And the parcels a2a agent's answer contains 'PX-A2A-9201 is in transit'
    And the parcels a2a agent's task has an artifact where:
      | name        | parcel-status |
      | data.status | IN_TRANSIT    |

  Scenario: A hold asks until which day, and holds the parcel on the reply
    Given a seeds/a2a-hold.yaml db seed
    When a message is sent to the parcels a2a agent:
      """
      Please hold PX-A2A-9202 at its depot.
      """
    Then the parcels a2a agent's task is input-required
    And the parcels a2a agent's answer contains 'Until which day should PX-A2A-9202 be held?'
    When a reply is sent to the parcels a2a agent:
      """
      2026-10-05
      """
    Then the parcels a2a agent's task is completed
    And the parcels a2a agent's answer contains 'PX-A2A-9202 is held at its depot until 2026-10-05.'
    And a selection of rows is retrieved from the parcels.parcels table where:
      | reference | PX-A2A-9202 |
      | status    | ON_HOLD     |
    And the selection has 1 row

  Scenario: A parcel out for delivery can no longer be held
    Given a seeds/a2a-too-late.yaml db seed
    When a message is sent to the parcels a2a agent:
      """
      Hold PX-A2A-9203 until 2026-10-05, please.
      """
    Then the parcels a2a agent's task is failed
    And the parcels a2a agent's answer contains 'PX-A2A-9203 is out for delivery: it can no longer be held.'

  Scenario: The agent streams its work, over HTTP+JSON
    Given a seeds/a2a-stream.yaml db seed
    And the parcels-rest a2a agent with the following properties:
      | url       | http://localhost:8400 |
      | transport | HTTP+JSON             |
    When a message is streamed to the parcels-rest a2a agent:
      """
      Where is PX-A2A-9204?
      """
    Then the parcels-rest a2a agent's stream received an update where:
      | kind    | status                 |
      | state   | working                |
      | message | Looking up PX-A2A-9204 |
    And the parcels-rest a2a agent's stream received an update where:
      | kind          | artifact      |
      | artifact.name | parcel-status |
    And within 10s the parcels-rest a2a agent's task is completed

  Scenario: A hold can be called off before it is answered
    Given a seeds/a2a-cancel.yaml db seed
    When a message is sent to the parcels a2a agent:
      """
      Hold PX-A2A-9205
      """
    Then the parcels a2a agent's task is input-required
    When the parcels a2a agent is asked to cancel its task
    Then the parcels a2a agent's task is canceled

  Scenario: The agent does not know a parcel that was never registered
    When a message is sent to the parcels a2a agent:
      """
      Where is PX-A2A-9299?
      """
    Then the parcels a2a agent's task is rejected
    And the parcels a2a agent's answer contains 'There is no parcel PX-A2A-9299.'

  Scenario: For a parcel abroad, the partner carrier's agent is asked
    Given a seeds/a2a-abroad.yaml db seed
    And the mocked partner-carrier service with the following properties:
      | url | http://${sys:local.host}:8087 |
    When a message is sent to the parcels a2a agent:
      """
      Where is PX-A2A-9206?
      """
    Then the parcels a2a agent's task is completed
    And the parcels a2a agent's answer contains 'PX-A2A-9206 is with our partner carrier, who says: PX-A2A-9206 cleared customs in Basel'
    And the mocked partner-carrier a2a agent was sent a message containing 'PX-A2A-9206' 1 time
