---
title: Mock dependencies
description: Replace the services your service calls with WireMock, verify the requests it sent, and check both sides of each dependency's OpenAPI contract with the Axx WireMock image.
---

Your service calls other services. In an acceptance test you replace them with [WireMock](https://wiremock.org/) mocks, which gives you two things: the responses are under your control, and you can check exactly what your service sent, down to the signatures of its webhooks ([Test webhooks and tokens](/guides/webhooks-and-tokens/)).

## Define the stubs

Stubs are ordinary WireMock mapping files. Axx does not create stubs; it verifies traffic. Mount the mappings into a WireMock container:

```json title="infra/wiremock/mappings/postcode-undeliverable.json"
{
  "name": "postcode-undeliverable",
  "priority": 1,
  "request": {
    "method": "GET",
    "urlPathPattern": "/v1/postcodes/[A-Z]{2}/999[^/]*"
  },
  "response": {
    "status": 200,
    "headers": { "Content-Type": "application/json" },
    "body": "{\"postcode\": \"{{request.pathSegments.[3]}}\", \"country\": \"{{request.pathSegments.[2]}}\", \"deliverable\": false, \"reason\": \"no delivery to this postcode\"}",
    "transformers": ["response-template"]
  }
}
```

```yaml title="infra/compose.yaml (excerpt)"
services:
  address-service:
    image: wiremock/wiremock:3.13.0
    ports: ["8081:8080"]
    volumes: ["./wiremock:/home/wiremock"]
```

Configure your service to call `http://localhost:8081` (or the compose service name) instead of the real dependency.

## Verify what your service sent

Register the mock in the scenario, trigger the behavior, then check the requests WireMock received:

```gherkin
Feature: Address check

  Background:
    Given the parcels service with the following properties:
      | url | http://localhost:8400 |
    And the mocked addresses service with the following properties:
      | url | http://localhost:8081 |

  Scenario: The address service is asked with the API key
    Given a POST request to /api/parcels
    And a request payload using an application/json content example
    And the request payload properties are:
      | reference          | PX-ADR-1101 |
      | recipient.postcode | "53111"     |
    When the request is executed
    Then the response status code is 201
    And the mocked GET request to /v1/postcodes/DE/53111 named postcode-check was received by addresses
    And the mocked request named postcode-check was received exactly 1 time
    And the header X-Api-Key for mocked request named postcode-check on addresses is 'example-address-key'
```

`the mocked GET request to /v1/postcodes/DE/53111 named postcode-check was received by addresses` registers a request pattern (method and exact URL, including the query string) under a name you choose, and checks it was received at least once. Later steps refer to it by that name; the [mock pack reference](/references/packs/mock/) has the ones that check counts, absence and headers.

```gherkin
Scenario: Invalid registrations never reach the address service
  Given the OpenAPI validation levels are:
    | validation.request.body.schema.minimum | IGNORE |
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload properties are:
    | reference          | PX-ADR-1103 |
    | weightGrams        | 0           |
    | recipient.postcode | "12489"     |
  When the request is executed
  Then the response status code is 400
  And the mocked GET request to /v1/postcodes/DE/12489 named skipped-check was not received
```

### What a request sent

When requests share a URL, like every courier booking going to `/v1/collections`, check what a request sent: the properties of its JSON body, or the fields of its form. The checks stay with the named request, so its counts count only the requests that sent that:

```gherkin
Scenario: The courier collects an express parcel without being told who it goes to
  Given the mocked courier service with the following properties:
    | url | http://localhost:8082 |
  And a POST request to /api/parcels
  And a request payload using an application/json content example named 'Express parcel'
  And the request payload property reference is 'PX-REG-1401'
  When the request is executed
  Then the response status code is 201
  And the mocked POST request to /v1/collections named collection was received by courier
  And the payload properties for mocked request named collection on courier are:
    | reference          | PX-REG-1401 |
    | deliverTo.postcode | "75001"     |
    | recipient          | undefined   |
  And the mocked request named collection on courier was received exactly 1 time
```

- **A property is a JSONPath** (`deliverTo.postcode`, `$.lines[0].reference`), and values compare as text: `800` matches the number 800, `"75001"` the string.
- **`undefined` checks that something was left out**, such as a recipient's name the courier must never get.
- **Form fields** of a form-encoded body are checked the same way: `the form fields for mocked request named pickup-notice are:`, with a field and its value a row.

When a request's query changes from call to call, such as a request ID, name the request by its path, and check the query parameters that matter:

```gherkin
Then the mocked POST request to path /v1/collections named collection was received by courier
And the query parameters for mocked request named collection on courier are:
  | slot      | same-day  |
  | reference | undefined |
```

A request named by its path matches whatever its query string; its query parameters narrow it, and its counts, like its body's properties.

### What your service did not send

A check that something did not happen also passes when the service never got that far, so check what it did do too, like the response. Every scenario's requests count, so the check names the scenario's own data:

```gherkin
Scenario: The courier is not called about a cancelled standard parcel
  Given a seeds/cancel-standard.yaml db seed
  And the mocked courier service with the following properties:
    | url | http://localhost:8082 |
  And a DELETE request to /api/parcels/PX-REG-1602
  When the request is executed
  Then the response status code is 204
  And none of the mocked DELETE requests to path /v1/collections on courier have the query parameters:
    | reference | PX-REG-1602 |
```

- **`none of the mocked ... requests to path ... have ...`** checks that no request to the path has every row of its table: `the query parameters`, `the payload properties` or `the form fields`. Put the scenario's own data in the table, next to what must not be there.
- **`the mocked ... request to path ... named ... was not received`** checks that no request went to a path at all, whatever its query string. Use it for a path that is the scenario's own, like one with its parcel's reference.

:::caution[Journals persist]
WireMock keeps its request journal between scenarios, and between runs for as long as it keeps running, and scenarios run in parallel. Match on something unique to the scenario (a postcode or an id in the URL, a header, a property of the body) so one scenario never counts another scenario's requests. A count such as `exactly 1 time` also counts the requests of earlier runs while the mock keeps running, as it does between runs with `axx up`. See [Isolate test data](/guides/isolate-test-data/).
:::

## Mock gRPC dependencies

The Axx WireMock image also carries [WireMock's gRPC extension](https://wiremock.org/docs/grpc/), so a gRPC dependency is mocked like a REST one. Give the mock a `grpc` folder with the descriptor set of the dependency's services (`protoc --include_imports --descriptor_set_out=rating.dsc rating.proto`), and stub each method as a POST to `/<package>.<Service>/<Method>`, with its request and answer as proto JSON:

```json title="infra/rating/mappings/rating-switzerland.json"
{
  "name": "rating-switzerland",
  "request": {
    "method": "POST",
    "urlPath": "/parcels.rating.v1.Rates/Quote",
    "bodyPatterns": [{"matchesJsonPath": "$[?(@.country == 'CH')]"}]
  },
  "response": {"status": 200, "jsonBody": {"surchargeCents": 1250, "deliveryDays": 3}}
}
```

- **Another status than OK** is a response header: `"headers": {"grpc-status-name": "NOT_FOUND", "grpc-status-reason": "the carrier does not deliver to AQ"}`.
- **The mock steps check the calls** as they check REST requests: WireMock records each call as a POST to its method's path, with its request as JSON.

```gherkin
Scenario: A parcel beyond the EU is priced by the partner carrier
  Given the mocked rating service with the following properties:
    | url | http://localhost:8084 |
  And a POST request to /api/quotes
  And a request payload using an application/json content example named 'Express abroad'
  And the request payload properties are:
    | weightGrams        | 2350   |
    | recipient.postcode | "8001" |
    | recipient.country  | CH     |
  When the request is executed
  Then the response status code is 200
  And the mocked POST request to path /parcels.rating.v1.Rates/Quote named rate was received by rating
  And the payload properties for mocked request named rate on rating are:
    | country      | CH      |
    | weightGrams  | 2350    |
    | serviceLevel | EXPRESS |
```

Every scenario's calls to a method share its path, so the payload check names data of the scenario's own, here the weight. The gRPC extension runs when WireMock's root has a `grpc` folder; the OpenAPI validation does not apply to gRPC calls. To call a gRPC service yourself, see [Call gRPC services](/guides/test-grpc/).

## Mock GraphQL services and subgraphs

The Axx WireMock image mocks a GraphQL service, or a federated subgraph, **field by field from its schema**. Set `GRAPHQL_SCHEMA_SOURCE` to its SDL, and the mock answers `POST /graphql` by running each operation against the schema: a field's value comes from its parent's value when that has it, and otherwise from a stub. Whatever fields a query selects, it gets an answer, which is what a gateway's changing query plans need.

```yaml title="infra/compose.yaml (excerpt)"
shop-directory:
  image: ghcr.io/nimbusxr/axx-wiremock:<version>   # a version with GraphQL mocks
  environment:
    GRAPHQL_SCHEMA_SOURCE: /home/wiremock/graphql/shops.graphql
  volumes:
    - './shop-directory:/home/wiremock'
```

Stubs are ordinary WireMock mappings, matched with WireMock's matchers:

- **A field** is a POST to `/graphql/<Type>/<field>`, whose body is `{"arguments": {...}, "source": {...}}` (the field's arguments, and its parent's value).
- **An entity** of a subgraph (`_entities`) is a POST to `/graphql/_entities/<Type>`, whose body is its representation, such as `{"__typename": "Shop", "id": "alder-and-ash"}`.
- **The stub's JSON body is the value.** A `graphql-error` header makes the field an error instead, with `graphql-error-code` as its `extensions.code`.
- **A field without a value is null,** and an entity without a stub too.

```json title="infra/shop-directory/mappings/shop-alder-and-ash.json"
{
  "name": "shop-alder-and-ash",
  "request": {
    "method": "POST",
    "urlPath": "/graphql/_entities/Shop",
    "bodyPatterns": [{"equalToJson": {"id": "alder-and-ash"}, "ignoreExtraElements": true}]
  },
  "response": {"status": 200, "jsonBody": {"name": "Alder & Ash", "tier": "STANDARD"}}
}
```

A subgraph's SDL makes the mock a subgraph: it answers `_service { sdl }` and `_entities`, so a gateway composes and plans with it. The journal records each operation the mock received, with its query and variables, so the mock steps check what was asked:

```gherkin
And the mocked POST request to path /graphql named shop-lookup was received by shop-directory
And the payload properties for mocked request named shop-lookup on shop-directory are:
  | variables.representations[0].id | alder-and-ash |
```

A stub of a whole operation on `/graphql` (for example, matching `$.query`) wins over the field-by-field answers, for an outage or an answer the schema cannot give. The OpenAPI validation does not apply to GraphQL calls. To send operations to a GraphQL service yourself, see [Call GraphQL services](/guides/test-graphql/).

## Mock AI models

The Axx WireMock image mocks the models a service asks, in the format of the request: OpenAI's API (and the servers that speak it), Anthropic's, Gemini's, Bedrock's and Ollama's, with their streams and their errors. A stub's `model-request` matcher says which requests it answers (`about` a text of the conversation), and its `model-answer` transformer renders its answer, written the same way for every provider:

```json title="infra/models/mappings/address-note.json"
{
  "request": {"customMatcher": {"name": "model-request", "parameters": {"about": "Birkenallee 3"}}},
  "response": {
    "transformers": ["model-answer"],
    "jsonBody": {"json": {"name": "Mara Lindqvist", "street": "Birkenallee 3", "postcode": "01067", "city": "Dresden", "country": "DE"}}
  }
}
```

The mock pack's model steps check what the service asked the model: the texts, the tools and the schema. See [Test AI features](/guides/test-ai-features/).

## Check the dependency's contract

A mock that answers something the real API never would makes a test pass for the wrong reason, and a service that calls its dependency wrongly only finds out in production. The `ghcr.io/nimbusxr/axx-wiremock` image is WireMock with Axx's OpenAPI validation extension. It checks every call to the mock against the dependency's OpenAPI document: your service's request, and the stub's response.

```yaml title="infra/compose.yaml (excerpt)"
services:
  address-service:
    image: ghcr.io/nimbusxr/axx-wiremock:0.1
    ports: ["8081:8080"]
    environment:
      OPENAPI_SPEC_SOURCE: /var/openapi/address-service.yaml
      OPENAPI_VALIDATION_MODE: report
    volumes:
      - ./wiremock:/home/wiremock
      - ./openapi:/var/openapi:ro
```

`OPENAPI_SPEC_SOURCE` is the dependency's document (a path inside the container or a URL). A stub can name another one with `"metadata": {"openApiSpecSource": "..."}`, or opt out with `"openApiValidation": false`. In `report` mode your service gets the stub's answer unchanged, as it would from the real dependency, and Axx reports what broke the contract:

- A mock step that checks the call fails, and names the rule:

  ```console
      ✗ And the mocked GET request to /v1/postcodes/DE/44444 named zone-lookup was received by addresses
          The GET /v1/postcodes/DE/44444 request named zone-lookup broke the contract of the mocked addresses service (/var/openapi/address-service.yaml):
            - validation.response.body.schema.additionalProperties (response): property 'surcharge' is not defined in the schema and the schema does not allow additional properties
  ```

  `(response)` means the stub broke the contract; `(request)` means your service did.

- A call that breaks the contract and that no scenario checks fails the run, after the scenarios:

  ```console
  Run failures:
    ✗ Calls to the mocked addresses service (http://localhost:8081) broke its contract, and no scenario checked them:  (mock)
          GET /v1/postcodes/DE/55555 (/var/openapi/address-service.yaml)
            - validation.response.body.schema.additionalProperties (response): property 'surcharge' is not defined in the schema and the schema does not allow additional properties
  ```

Axx only reads WireMock's request journal, so scenarios running in parallel cannot affect each other's checks. It reads the journals of the mocks the run's scenarios register: register the mocked service in the `Background` of every feature whose scenarios make the calls, so Axx knows to look.

### Relax a check

Every rule is an `ERROR` until you relax it. Relaxing is how you tell Axx a deviation is known: `WARN` logs the finding on the checking step instead of failing it, `INFO` and `IGNORE` drop it. The rules have the same keys as your own service's contract (`validation.request.body.schema.required`, `validation.response.status.unknown`, ...), and a key also covers the keys below it. The settings are separate: [`the OpenAPI validation levels are:`](/guides/validate-openapi/) never relaxes a dependency's contract.

Relax where the deviation lives:

| Where | Relaxes | Use it for |
| --- | --- | --- |
| `"openApiValidationLevels": {"validation.response.body": "WARN"}` in a stub's metadata | the calls that stub answers, in every scenario and in the end-of-run check | a stub whose answer is off-contract wherever it is used |
| `OPENAPI_VALIDATION_LEVELS` on the container, e.g. `validation.response.body.schema.additionalProperties=WARN` | every call to that mock | a known gap between the dependency's document and its behavior |
| the scenario step below | the calls that scenario's mock steps check | a scenario that exercises an off-contract call on purpose, next to the behavior it checks |

In the example, the address service starts sending a field its document does not list yet, and quoting must keep working. The relaxation sits in that scenario, which checks the call:

```gherkin
Scenario: Quotes keep working when the address service sends a field it has not documented
  Given the OpenAPI validation levels for the mocked addresses service are:
    | validation.response.body.schema.additionalProperties | WARN |
  And a POST request to /api/quotes
  And a request payload using an application/json content example named 'Domestic parcel'
  And the request payload property recipient.postcode is '"80995"'
  When the request is executed
  Then the response status code is 200
  And the response payload property zone is 'DE-8'
  And the mocked GET request to /v1/postcodes/DE/80995 named zone-lookup was received by addresses
```

The scenario step only reaches calls the scenario checks with a mock step: the end-of-run check cannot tell which scenario made a call. When a scenario relaxes a mocked service but checks none of its calls, Axx warns at the end of that scenario, and if one of its calls breaks the rule anyway, the run report names the scenario that meant to relax it.

The more specific place wins. Without the extension's `report` mode (`fail`, its default), a call that breaks the contract also gets an HTTP 500 listing the findings; Axx reports it the same way. All settings, the admin endpoints and how the image is versioned are in the [extension's README](https://github.com/nimbusxr/axx/tree/main/extensions/wiremock-openapi).

Keep mock response bodies schema-valid as the provider's API evolves with [fixture factories](/guides/fixture-factories/): the `json` family can use an OpenAPI component as its schema, which is how the example generates `postcode-remote.json` from `infra/wiremock/__files/postcodes.factory.yaml`.
