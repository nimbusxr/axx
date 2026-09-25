---
title: Validate against OpenAPI
description: Check every request and response against your OpenAPI document, build payloads from its examples, and relax individual rules for negative tests.
---

Give a REST service an `openapi` property and Axx validates every request it sends and every response it receives against that document. A violation fails the `When the request is executed` step and names the rule that broke. Your OpenAPI document stays the contract, and the acceptance suite proves the service honors it.

## Turn it on

```gherkin
Background:
  Given the parcels service with the following properties:
    | url     | http://localhost:8400              |
    | openapi | http://localhost:8400/openapi.json |
```

`openapi` is a URL or a file path (resolved against `resources`), in JSON or YAML. OpenAPI 3.0 and 3.1 are supported.

Point it at the document your service serves (`/openapi.json`, `/v3/api-docs`) or at the file in your repository. Do not point it at a third party's live URL: if it is unreachable, your tests fail for reasons that have nothing to do with your service. Mock third parties instead: the WireMock image that mocks them checks their contracts, with settings of their own ([Mock dependencies](/guides/mock-dependencies/#check-the-dependencys-contract)).

## Build payloads from examples

Payloads in Gherkin tables get long. Start from an example in the OpenAPI document and change only what the scenario is about:

```yaml title="openapi.yaml (excerpt)"
paths:
  /api/parcels:
    post:
      requestBody:
        content:
          application/json:
            examples:
              Standard parcel:
                value:
                  reference: PX-EXAMPLE-1
                  sender: shop-example
                  weightGrams: 1200
                  serviceLevel: STANDARD
                  recipient:
                    name: Ada Lovelace
                    street: Invalidenstrasse 116
                    city: Berlin
                    postcode: "10115"
                    country: DE
```

```gherkin
Scenario: A shop registers a parcel
  Given a POST request to /api/parcels
  And a request payload using an application/json content example named 'Standard parcel'
  And the request payload properties are:
    | reference   | PX-REG-1001 |
    | weightGrams | 2500        |
  When the request is executed
  Then the response status code is 201
```

- `a request payload using an application/json content example` takes the operation's first example; add `named '<name>'` to pick one.
- `a request payload using an application/json empty content template` starts from an empty body instead.
- Examples can use `externalValue` to keep large payloads in their own files.

### Setting values in a payload

Keys are JSONPath expressions (`reference`, `recipient.postcode`, `items[0].sku`). A value keeps the type of the property it replaces, double quotes make it a string, `null` sets JSON null and `undefined` removes the property. The [step reference](/references/steps/rest/#restrequestproperties) has the exact rules.

Double-quote values that look like numbers but are strings in the schema, such as postcodes (`"50667"`) or long numeric identifiers, so they stay exact strings.

## Validation levels

Each rule has a level: `ERROR` fails the step, while `WARN`, `INFO` and `IGNORE` let it pass. Everything is `ERROR` by default. `FAIL` is accepted as an alias of `ERROR`.

For a negative test, where you send an invalid request on purpose to check that the service rejects it, relax only the rule you are breaking, only in that scenario:

```gherkin
Scenario: Parcels over 30 kg are refused
  Given the OpenAPI validation levels are:
    | validation.request.body.schema.maximum | IGNORE |
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload properties are:
    | reference   | PX-REG-1005 |
    | weightGrams | 31000       |
  When the request is executed
  Then the response status code is 400
  And the response payload property detail is 'weightGrams must be at most 30000'
```

The document says `weightGrams` is at most 30000, so without the relaxed level the request step would fail on the contract violation, and the scenario could never check how the service answers. Only the `maximum` rule is relaxed; the rest of the body is still checked.

With several REST services, name the one to relax:

```gherkin
Given the OpenAPI validation levels on parcels are:
  | validation.request.body.schema.maximum | IGNORE |
```

Levels apply when the request is executed, so a scenario can relax a rule for one request and restore it for the next by setting `ERROR` again.

Common keys:

| Key | Checks |
| --- | --- |
| `validation.request.body` | the request body against the schema |
| `validation.request.body.schema.<keyword>` | one schema keyword in the request body (`maximum`, `pattern`, `required`, ...) |
| `validation.request.parameter.missing` | required path parameters (query and header parameters have `validation.request.parameter.query.missing` and `validation.request.parameter.header.missing`) |
| `validation.request.security.missing` | required security schemes |
| `validation.response.body` | the response body against the schema |
| `validation.response.body.schema.required` | required properties in the response |
| `validation.response.body.schema.type` | property types in the response |

The failure message of a violated rule includes its key, so you can copy it into the table.

### Defaults for the whole suite

`openapi.levels` in `axx.yaml` sets suite-wide defaults. Use it for a known gap in a document you do not control, not to silence your own contract:

```yaml title="axx.yaml"
openapi:
  levels:
    validation.request.security.missing: IGNORE
```

## OpenAPI 3.1 and `nullable`

OpenAPI 3.1 replaced `nullable: true` with JSON Schema type unions. In a document that declares `openapi: 3.1.0`, a schema that uses `nullable` does not compile, so every request or response validated against it fails with a `...schema.processingError`, whatever its values:

```yaml
# openapi: 3.1.0
nickname:
  type: [string, "null"]   # not: type: string + nullable: true
```

`nullable` is still correct in `openapi: 3.0.x` documents.

## The other side: your dependencies' contracts

This page covers your service's own contract. The services your service calls have contracts too: run their WireMock mocks with the OpenAPI validation extension, and every call your service makes and every stubbed answer is checked against the dependency's document. Those checks have levels of their own; `the OpenAPI validation levels are:` never relaxes them. See [Mock dependencies](/guides/mock-dependencies/#check-the-dependencys-contract).
