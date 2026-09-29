---
title: Call JSON-RPC services
description: Call your JSON-RPC 2.0 services with params from tables or doc strings, check their results and errors, and check every call against the service's OpenRPC document, with levels to relax a rule.
---

The `jsonrpc` pack calls your JSON-RPC 2.0 services over HTTP, and checks the results and errors they answer. With the service's [OpenRPC](https://open-rpc.org/) document, every call is also checked against its contract, as the REST pack checks requests against OpenAPI.

```gherkin
Scenario: A depot holds a parcel until the day the recipient chose
  Given a seeds/depot-hold.yaml db seed
  And the depots jsonrpc service with the following properties:
    | url     | http://localhost:8400/rpc |
    | openrpc | rpc.discover              |
  When the parcel.hold method is called on the depots jsonrpc service with the following params:
    | reference | PX-RPC-7102          |
    | until     | 2026-10-05           |
    | reason    | recipient on holiday |
  Then the depots jsonrpc service's result has the following properties:
    | status    | ON_HOLD    |
    | heldUntil | 2026-10-05 |
```

Add the pack to the project with `axx pack add jsonrpc` ([Choose packs](/guides/use-packs/)).

## Register the service

| Property | What it is |
| --- | --- |
| `url` | The URL calls are posted to (required). |
| `header.<name>` | A header sent with every call, such as `header.Authorization`. |
| `timeout` | How long a call may take. Default `10s`. |
| `openrpc` | The service's OpenRPC document: a file of the project, a URL, or `rpc.discover` to ask the service for it with the method OpenRPC defines. |

`${env:..}` values are masked in logs and failures, and `${token:..}` names a token the scenario registered.

## Call a method

```gherkin
When the parcel.get method is called on the depots jsonrpc service with the params:
  """
  ["PX-RPC-7101"]
  """
When the parcel.hold method is called on the depots jsonrpc service with the following params:
  | reference | PX-RPC-7102 |
  | until     | 2026-10-05  |
When the depot.list method is called on the depots jsonrpc service
```

- **A table gives params by name,** set as the REST pack's request properties are: a row can be a path into a param (`address.postcode`), numbers and booleans are JSON's, double quotes make a value text, `null` is null and `undefined` leaves it out. With an OpenRPC document, a param whose schema says `string` takes the cell as text.
- **A doc string** gives an object (by name) or an array (by position).
- **An error** does not fail the step that makes the call: check it. Only a service that does not answer JSON-RPC 2.0, such as an HTML error page, fails it.

## Check the result or the error

```gherkin
Then the depots jsonrpc service's result has the following properties:
  | reference | PX-RPC-7101 |
  | heldUntil | undefined   |
Then the depots jsonrpc service's result is 'true'
Then the depots jsonrpc service answered the error 4009 with a message containing 'can no longer be held'
And the depots jsonrpc service's error has the following properties:
  | data.status | OUT_FOR_DELIVERY |
```

- `result is` compares a text result's text, or any other result's JSON.
- The error's properties are its `code`, its `message` and its `data`.
- The checks read the service's last call in the scenario.

## Check calls against OpenRPC

With an `openrpc` row, every call's params are checked against its method before it is sent, and every result and error when it comes back. A call that breaks the document fails the step that makes it, and names the rule it broke:

| Key | Found when |
| --- | --- |
| `validation.method.unknown` | the document has no such method |
| `validation.params.structure` | the params are by name for a method that takes them by position (`paramStructure`), or the other way round |
| `validation.params.missing` | a required param is missing |
| `validation.params.unknown` | a param the method does not have |
| `validation.params.schema.<keyword>` | a param breaks a keyword of its schema (`required`, `type`, `pattern`...) |
| `validation.result.schema.<keyword>` | the result breaks a keyword of its schema |
| `validation.error.unknown` | the error is not one the method declares, nor one of JSON-RPC's own (-32700, -32600 to -32603, -32000 to -32099) |

The document's schemas are JSON Schema, draft 7 unless they say otherwise, and its `$ref`s reach its components and other files.

### Relax a rule

`ERROR` fails the step, `WARN` and `INFO` log the finding, and `IGNORE` drops it. Everything is `ERROR` by default. A scenario that sends a broken call on purpose, to see what the service answers, relaxes only what it breaks:

```gherkin
Scenario: A hold without its day is refused
  Given the OpenRPC validation levels are:
    | validation.params.missing | IGNORE |
  When the parcel.hold method is called on the depots jsonrpc service with the following params:
    | reference | PX-RPC-7104 |
  Then the depots jsonrpc service answered the error -32602 with a message containing 'until'
```

A key also sets the keys below it (`validation.params` covers `validation.params.schema.pattern`), and the most specific key set wins. `packs.jsonrpc.openrpc.levels` in `axx.yaml` sets the defaults the scenarios' levels are merged over:

```yaml title="axx.yaml"
packs:
  jsonrpc:
    openrpc:
      levels:
        validation.error.unknown: WARN
```

See the [jsonrpc pack's reference](/references/packs/jsonrpc/) for every step.
