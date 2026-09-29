---
title: Call GraphQL services
description: Send queries, mutations and subscriptions to your GraphQL services, a federated graph's gateway or a subgraph alone, check their data and errors against the schema, and mock the subgraphs of other teams field by field.
---

The `graphql` pack sends queries, mutations and subscriptions to GraphQL services and checks what they answer. A federated graph's gateway and a subgraph alone are GraphQL services alike: register each at its URL, and the same steps test your subgraph on its own and the graph through the gateway.

```gherkin
Scenario: A parcel through the graph comes with its shop
  Given a seeds/graph-shop.yaml db seed
  And the graph graphql service with the following properties:
    | url    | http://localhost:4000/graphql |
    | schema | introspection                 |
  When the graphql/parcel-with-shop.graphql query is sent to the graph graphql service with the following variables:
    | reference | PX-GQL-7205 |
  Then the graph graphql service answered without errors
  And the graph graphql service's data has the following properties:
    | parcel.status    | IN_TRANSIT  |
    | parcel.shop.name | Alder & Ash |
```

Add the pack to the project with `axx pack add graphql` ([Choose packs](/guides/use-packs/)).

## Register the service

| Property | What it is |
| --- | --- |
| `url` | The URL operations are posted to (required). |
| `schema` | Its schema: an SDL file of the project, a URL, or `introspection` to ask the service. Operations and answers are checked against it. |
| `header.<name>` | A header sent with every operation, such as `header.Authorization`. |
| `timeout` | How long an operation may take. Default `10s`. |
| `subscriptions` | `graphql-ws` (the default: the `graphql-transport-ws` protocol) or `sse` (server-sent events). |
| `subscriptions url` | Where subscriptions go. Default: the `url`, with `ws://` or `wss://` for graphql-ws. |

`${env:..}` values are masked in logs and failures, and `${token:..}` names a token the scenario registered ([Test webhooks and tokens](/guides/webhooks-and-tokens/)). A subgraph's SDL may use the federation directives (`@key`, `@link`...).

## Send queries and mutations

Keep operations in `.graphql` files, and give their variables in a table:

```graphql title="graphql/hold-parcel.graphql"
mutation HoldParcel($reference: ID!, $until: String!, $reason: String) {
  holdParcel(reference: $reference, until: $until, reason: $reason) {
    reference
    status
    heldUntil
  }
}
```

```gherkin
When the graphql/hold-parcel.graphql mutation is sent to the parcels graphql service with the following variables:
  | reference | PX-GQL-7202          |
  | until     | 2026-10-05           |
  | reason    | recipient on holiday |
When a query is sent to the parcels graphql service:
  """
  { parcel(reference: "PX-GQL-7201") { status } }
  """
```

- **A variable takes the type the operation declares:** `10115` is text for a `String!` variable and a number for an `Int!`. Double quotes make a value text, `null` is null, and `undefined` leaves the variable out. A row can be a path into an input object (`address.postcode`).
- **The step says what the operation is:** sending a mutation file with "query" fails, which keeps the feature file honest about what it does.
- **Errors are the answer's:** an answer with errors does not fail the step that sends the operation. Check them.

## Check the answer

```gherkin
Then the parcels graphql service answered without errors
And the parcels graphql service's data has the following properties:
  | holdParcel.status    | ON_HOLD    |
  | holdParcel.heldUntil | 2026-10-05 |
Then the parcels graphql service answered an error where:
  | path            | holdParcel   |
  | extensions.code | NOT_HOLDABLE |
```

- Data paths are dotted (`parcel.shop.name`) or JSONPath; `null` and `undefined` work as in the other property steps.
- An error's `path` is its fields and indexes joined with dots: `parcel.shop.name`, `parcels.0.reference`. The check passes when one of the answer's errors has every row.
- The checks read the service's last answer in the scenario.

## Subscriptions

Start a subscription before what makes the service send its messages:

```gherkin
Scenario: A shop's app hears its parcel go out for delivery
  Given a seeds/graph-scanned.yaml db seed
  And the graphql/parcel-scanned.graphql subscription is started on the parcels graphql service with the following variables:
    | reference | PX-GQL-7204 |
  And a depot-scans kafka event
  And the depot-scans kafka event key is PX-GQL-7204
  And the depot-scans kafka event payload is a kafka/scan-out-for-delivery.json resource
  When the depot-scans kafka event is published using schema schemas/depot-scan.avsc
  Then within 20s the parcels graphql service's subscription received a message where:
    | parcelScanned.status | OUT_FOR_DELIVERY |
```

- The check waits for a message whose data has those values: 10 seconds, or `within {duration}`. When the subscription has ended, it fails at once, saying why.
- **A subscription belongs to the scenario,** which ends it when it ends.

## Check against the schema

With a `schema` row, every operation and its variables are checked against the schema before they are sent, and every answer's data against the operation's types. An operation that breaks the schema is not sent; the step fails and names the rule it broke:

| Key | Found when |
| --- | --- |
| `validation.operation.<rule>` | the operation breaks a validation rule of the GraphQL spec (`FieldsOnCorrectType`, `KnownArgumentNames`, `ProvidedRequiredArguments`...) |
| `validation.variables.missing` | a required variable is missing |
| `validation.variables.type` | a variable is not of its type |
| `validation.response.nonNull` | a non-null field is null, without an error that explains it |
| `validation.response.type` | a field's value is not of its type |
| `validation.response.enum` | a value is not one of its enum's |
| `validation.response.missingField` | a field the operation selects is missing |
| `validation.response.unknownField` | the data has a field the operation does not select |

`ERROR` fails the step, `WARN` and `INFO` log the finding, and `IGNORE` drops it. A scenario that sends a broken operation on purpose, to see how the service refuses it, relaxes only what it breaks:

```gherkin
Scenario: The graph refuses a field it does not have
  Given the GraphQL validation levels are:
    | validation.operation.FieldsOnCorrectType | IGNORE |
  When a query is sent to the graph graphql service:
    """
    query SignedBy { parcel(reference: "PX-GQL-7299") { signedBy } }
    """
  Then the graph graphql service answered an error where:
    | message | Cannot query field "signedBy" on type "Parcel". |
```

A key also sets the keys below it (`validation.response` covers `validation.response.enum`), and the most specific key set wins. `packs.graphql.levels` in `axx.yaml` sets the defaults the scenarios' levels are merged over.

## Federated graphs

A federated graph has three parts to test, with the same steps:

- **Your subgraph on its own:** register it at its own URL. Queries, mutations and subscriptions reach your code directly, and its SDL is the contract.
- **The graph through its gateway:** register the gateway's URL. A query crosses your subgraph and the others, as the gateway plans it, which is where composition and entity resolution break.
- **Your service as a client of someone's graph:** mock that graph, and check what your service asked it.

In the example, a gateway ([Hive Gateway](https://the-guild.dev/graphql/hive/docs/gateway)) composes the service's own subgraph with a shops subgraph that another team owns, mocked by WireMock (below). Axx does not include a gateway: it is a container in your `compose.yaml`, like the one you run in production (Apollo Router, Cosmo Router, Hive Gateway...), and Axx only talks to its URL.

## Mock GraphQL services and subgraphs

The Axx WireMock image mocks GraphQL services and subgraphs **field by field, from their schema**: it runs each operation against the schema, so any query your service or a gateway sends is answered, whatever fields it selects. A subgraph's mock answers `_service` and `_entities`, so a gateway composes and plans with it as with the real one. See [Mock GraphQL services and subgraphs](/guides/mock-dependencies/#mock-graphql-services-and-subgraphs).

See the [graphql pack's reference](/references/packs/graphql/) for every step.
