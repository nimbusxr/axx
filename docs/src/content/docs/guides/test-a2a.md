---
title: Test A2A agents
description: "Send messages to your A2A agent as another agent does, over JSON-RPC, HTTP+JSON or gRPC; check its card, its tasks' states and artifacts, and what it streams; and mock the agents your service asks."
---

An A2A agent (Agent2Agent protocol) is an agent other agents ask for work. It publishes a card that says what it does and how to reach it, and it answers a message with a reply or a task: work that goes through states (working, input required, completed...) and ends with artifacts. The `a2a` pack tests your agent by doing what another agent does.

```gherkin
Scenario: A hold asks until which day, and holds the parcel on the reply
  Given a seeds/a2a-hold.yaml db seed
  And the parcels a2a agent with the following properties:
    | url | http://localhost:8400 |
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
```

Add the pack to the project with `axx pack add a2a` ([Choose packs](/guides/use-packs/)).

## Register the agent

| Property | What it is |
| --- | --- |
| `url` | The agent's URL. Its card is read at `/.well-known/agent-card.json` on its origin. |
| `card` | The agent's card, when it is not there: a file of the project, or a URL. |
| `transport` | How to reach the agent: `JSONRPC`, `HTTP+JSON` or `GRPC`, among the interfaces its card lists. Default: the card's first. |
| `header.<name>` | A header sent with every request, such as `header.Authorization`. |
| `timeout` | How long a call may take. Default `30s`. |
| `push url`, `push token` | Where the agent sends its tasks' updates (a webhook: a WireMock the mock pack checks), and the token it sends with them. |

The card is checked for what A2A 1.0 requires: its name, description, version, interfaces and input and output modes. Check what it says, and its skills:

```gherkin
Then the parcels a2a agent's card has the following properties:
  | name                   | Parcels agent |
  | capabilities.streaming | true          |
And the parcels a2a agent has the track-parcel skill
```

## Send messages

```gherkin
When a message is sent to the parcels a2a agent:
  """
  Where is PX-A2A-9201?
  """
When the a2a/hold-request.json message is sent to the parcels a2a agent
When a reply is sent to the parcels a2a agent:
  """
  2026-10-05
  """
When the parcels a2a agent is asked to cancel its task
```

- **A message starts a conversation,** and waits for the agent's answer: a reply, or a task that ended or waits for input.
- **A reply continues the agent's last task.** That is how a task that asks for input (`input-required`) gets it.
- **A file's message** is an A2A message in JSON, with text, data or file parts.

## Check the answer

```gherkin
Then the parcels a2a agent's task is completed
And the parcels a2a agent's answer contains 'PX-A2A-9201 is in transit'
And the parcels a2a agent's task has an artifact where:
  | name        | parcel-status |
  | data.status | IN_TRANSIT    |
```

- **States are written as people say them:** `submitted`, `working`, `completed`, `input-required`, `auth-required`, `failed`, `canceled` and `rejected`. A check waits for a task still at work (10 seconds, or `within {duration}`), and fails at once for a task in another state that will not change without a message.
- **The answer** is the agent's reply, or its task's status message and the text of its artifacts.
- **An artifact's properties** are its `name`, `description`, `text` (its text parts), `data` (its first data part: `data.status`) and `parts`.

## Streams

```gherkin
When a message is streamed to the parcels a2a agent:
  """
  Where is PX-A2A-9204?
  """
Then the parcels a2a agent's stream received an update where:
  | kind    | status                 |
  | state   | working                |
  | message | Looking up PX-A2A-9204 |
And within 10s the parcels a2a agent's task is completed
```

A streamed message takes the agent's answer as it comes: a status update has `kind` `status`, its `state` and its `message`; an artifact update has `kind` `artifact` and its `artifact` (`artifact.name`, `artifact.data.status`...). The checks wait for what the stream has not sent yet, and fail at once when it has ended. **A stream belongs to the scenario,** which ends it when it ends.

## Mock the agents your service asks

When your service asks another team's agent, the Axx WireMock image mocks it from its card, with a mapping file per answer, and the mock pack checks what your service asked it. See [Mock MCP servers and A2A agents](/guides/mock-dependencies/#mock-mcp-servers-and-a2a-agents).

See the [a2a pack's reference](/references/packs/a2a/) for every step.
