---
title: Test MCP servers
description: "Call the tools of your MCP server as an AI assistant calls them, over stdio or HTTP, with the tools' own schemas as the contract; read its resources and prompts; and mock the MCP servers your service calls."
---

An MCP server (Model Context Protocol) offers AI assistants tools, like `track_parcel`: the assistant reads each tool's name, description and argument schema, calls tools as it works, and gets their results back. The `mcp` pack tests your server by doing what an assistant does: it calls the tools and checks what they answer. No model is involved, so the scenarios are deterministic.

```gherkin
Scenario: An assistant tracks a parcel
  Given a seeds/mcp-track.yaml db seed
  And the parcels mcp server with the following properties:
    | url | http://localhost:8400/mcp |
  When the track_parcel tool is called on the parcels mcp server with the following arguments:
    | reference | PX-MCP-9101 |
  Then the track_parcel tool's result is not an error
  And the track_parcel tool's result has the following properties:
    | status       | OUT_FOR_DELIVERY |
    | serviceLevel | EXPRESS          |
```

Add the pack to the project with `axx pack add mcp` ([Choose packs](/guides/use-packs/)).

## Register the server

A server is reached over streamable HTTP (a `url`), or run over stdio (a `command`), as assistants run local servers.

Which one follows how the server's clients use it:

- **A server its clients start, over stdio.** Claude Code, Cursor and the other assistants run a local server as their own child process and talk to it through its stdin and stdout; nothing else can reach it. In a scenario, the scenario is the client, so the step runs it with `command`, the way the cli pack runs a command-line tool. It is not a service: nothing goes under `services:` in `axx.yaml`. Each scenario runs its own server, so a server that keeps data on disk can keep each scenario's apart (point its data folder, through `env.<NAME>`, at a folder of the scenario's own).
- **A server that runs on its own, over HTTP,** deployed where clients connect to it. axx starts it as a service in `axx.yaml` ([Manage the services under test](/guides/manage-services/)), and the step connects with `url`.

Test a server the way its clients reach it: a stdio server switched to HTTP only for its tests is tested over a transport its users never use.

```gherkin
Given the desk mcp server with the following properties:
  | command | docker compose exec -T app parcels mcp |
  | dir     | ../infra                               |
```

| Property | What it is |
| --- | --- |
| `url` | The server's streamable HTTP endpoint. |
| `command` | The command that runs the server over stdio, split into words without a shell. The scenario runs it, and ends it, and whatever it started, when it ends. |
| `dir` | The folder the command runs in, relative to the project. Default: a folder of the scenario's own. |
| `env.<NAME>` | An environment variable of the command. |
| `header.<name>` | A header sent with every request to the url, such as `header.Authorization`. |
| `timeout` | How long connecting, or a call, may take. Default `30s`. |
| `protocol version` | The MCP version to speak, like `2025-06-18`, to check that the server still answers an earlier one. Default: the latest the server speaks. |

The session agrees on the protocol version with the server: MCP 2026-07-28, which has no sessions, or an earlier one. `${env:..}` values are masked in logs and failures, and `${token:..}` names a token the scenario registered ([Test webhooks and tokens](/guides/webhooks-and-tokens/)).

## What an assistant sees

Before an assistant calls a tool, it reads the server's list of tools: their titles, descriptions and hints. Check them, and check that a tool the server must not offer is not there:

```gherkin
Then the parcels mcp server has the track_parcel tool with the following properties:
  | title                    | Track a parcel |
  | annotations.readOnlyHint | true           |
And the parcels mcp server does not have the cancel_parcel tool
```

## Call tools

```gherkin
When the hold_parcel tool is called on the parcels mcp server with the following arguments:
  | reference | PX-MCP-9102 |
  | until     | 2026-10-05  |
When the hold_parcel tool is called on the parcels mcp server with the mcp/hold-parcel.json arguments
```

- **An argument takes the type the tool's input schema gives it:** `01067` is text for a string, and a number for an integer. Double quotes make a value text, `null` is null, and `undefined` leaves it out. A row can be a path into an argument (`address.postcode`, `lines[0].reference`).
- **A file's arguments** are a JSON or YAML object.
- **A result that is an error does not fail the call,** nor does a call the server refuses: check them.

## Check results

```gherkin
Then the hold_parcel tool's result has the following properties:
  | status    | ON_HOLD    |
  | heldUntil | 2026-10-05 |
Then the hold_parcel tool's result is an error
And the hold_parcel tool's result contains 'it can no longer be held'
Then the cancel_parcel tool's call failed with the error code -32602
```

A tool fails in two ways, which the checks tell apart:

- **The tool ran, and says what went wrong:** its result is an error (`is an error`), with text the assistant reads, to tell the user or try again. A parcel that can no longer be held is one.
- **The server refused the call:** a JSON-RPC error, with its code (`failed with the error code`): `-32602` for a tool it does not have or invalid parameters.

`has the following properties` reads the tool's structured result, or the JSON of its text for a tool that answers JSON as text. `contains` reads its text and its structured result. The checks read the tool's latest result in the scenario; when two servers of the scenario have the same tool, name the server: `the track_parcel tool's result on the desk mcp server ...`.

## The schemas are the contract

A tool's arguments are checked against its input schema before they are sent, and its structured result against its output schema. A finding fails the step:

| Key | Found when |
| --- | --- |
| `validation.arguments.schema.<keyword>` | the arguments break the tool's input schema (`required`, `type`, `enum`...) |
| `validation.result.schema.<keyword>` | the structured result breaks the tool's output schema |
| `validation.result.missing` | a tool with an output schema answered no structured result |

A scenario that sends broken arguments on purpose, to see how the server refuses them, relaxes only what it breaks:

```gherkin
Scenario: A hold without its day is refused by the server too
  Given the MCP validation levels are:
    | validation.arguments.schema.required | IGNORE |
  When the hold_parcel tool is called on the parcels mcp server with the following arguments:
    | reference | PX-MCP-9102 |
  Then the hold_parcel tool's result is an error
```

`ERROR` (or `FAIL`) fails the step, `WARN` and `INFO` log the finding, and `IGNORE` drops it. A key also sets the keys below it, and `packs.mcp.levels` in `axx.yaml` sets the defaults the scenarios' levels are merged over.

## Resources and prompts

```gherkin
When the parcels://PX-MCP-9103/label resource is read from the parcels mcp server
Then the parcels://PX-MCP-9103/label resource has the following properties:
  | reference | PX-MCP-9103 |
When the delivery_update prompt is requested from the parcels mcp server with the following arguments:
  | reference | PX-MCP-9103 |
Then the delivery_update prompt contains 'parcel PX-MCP-9103'
```

A resource is read by its URI; `has the following properties` reads its text as JSON. A prompt's check reads the text of its messages.

## Mock the MCP servers your service calls

When your service calls another team's MCP server, the Axx WireMock image mocks it from a description of its tools, with a mapping file per answer, and the mock pack checks what your service called. See [Mock MCP servers and A2A agents](/guides/mock-dependencies/#mock-mcp-servers-and-a2a-agents).

See the [mcp pack's reference](/references/packs/mcp/) for every step.
