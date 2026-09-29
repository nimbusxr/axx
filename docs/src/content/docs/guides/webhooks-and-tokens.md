---
title: Test webhooks and tokens
description: Sign the webhooks a scenario sends your service, check the signatures of the webhooks your service sends, and authorize requests with JSON Web Tokens and OAuth 2.0 client credentials tokens.
---

Services call each other back: a courier calls your service as it delivers parcels, and your service calls each shop's system in turn. Each side signs what it sends, so the other can trust it. And an API that shops' systems call wants a token. The `rest` and `mock` packs cover both directions:

- **Sign the requests a scenario sends,** as the other side would.
- **Check the signatures of the webhooks your service sends,** on the WireMock that stands in for the receiver.
- **Authorize requests with a token:** one axx signs, or one it gets from a token endpoint.

```gherkin
Scenario: A shop hears of its parcel's delivery in a signed webhook
  Given a seeds/callback-delivered.yaml db seed
  And a POST request to /api/courier/callbacks
  And a request payload using an application/json content example named 'Delivered'
  And the request payload property reference is 'PX-WHK-9103'
  And the request is signed in the X-Courier-Signature header with the following properties:
    | key   | ${env:COURIER_CALLBACK_KEY} |
    | value | sha256={signature}          |
  When the request is executed
  Then the response status code is 204
  And the mocked POST request to path /webhooks/linden-and-lace named delivered was received by shops
  And the mocked request named delivered on shops is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}'
```

## Sign what a scenario sends

A webhook is signed with an HMAC of what it sends, in a header. A table says how:

```gherkin
Given the request is signed in the X-Courier-Signature header with the following properties:
  | key   | ${env:COURIER_CALLBACK_KEY} |
  | value | sha256={signature}          |
```

| Row | What it takes | Default |
| --- | --- | --- |
| `key` | the secret the signature is made with (required) | |
| `algorithm` | `hmac-sha256`, `hmac-sha1` or `hmac-sha512` | `hmac-sha256` |
| `signs` | what is signed: a template of `{body}`, `{timestamp}` (Unix seconds), `{method}` and `{path}` (with the query) | `{body}` |
| `encoding` | `hex` or `base64` | `hex` |
| `value` | the header's value: a template of `{signature}` and `{timestamp}` | `{signature}` |
| `timestamp header` | a header that also carries `{timestamp}` | |

The table covers the usual forms:

| Service | `signs` | `value` | Other rows |
| --- | --- | --- | --- |
| GitHub (`X-Hub-Signature-256`) | `{body}` | `sha256={signature}` | |
| Stripe (`Stripe-Signature`) | `{timestamp}.{body}` | `t={timestamp},v1={signature}` | |
| Slack (`X-Slack-Signature`) | `v0:{timestamp}:{body}` | `v0={signature}` | `timestamp header` is `X-Slack-Request-Timestamp` |

[Standard Webhooks](https://www.standardwebhooks.com) have a step of their own. It sets the `webhook-id`, `webhook-timestamp` and `webhook-signature` headers, with a key that is `whsec_` and the key in base64:

```gherkin
Given the request is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}'
```

The signature is made when the request is executed, over the body exactly as it is sent. Like the other request steps, both take `for 2nd ordered request` and `for request on <service>`.

## Check what your service signed

The receiving end of your service's webhooks is a WireMock ([Mock dependencies](/guides/mock-dependencies/)). Name the request first, then check its signature with the same table, or as a Standard Webhook:

```gherkin
Then the mocked POST request to path /webhooks/linden-and-lace named delivered was received by shops
And the payload properties for mocked request named delivered on shops are:
  | reference | PX-WHK-9103 |
And the mocked request named delivered on shops is signed as a standard webhook with the key '${env:SHOP_WEBHOOK_KEY}'
```

Axx signs what WireMock received, byte for byte, with its method and its path, and compares the signatures. `{timestamp}` comes from the header's value or from the timestamp header. The check fails unless the name matches a request, and every request it matches must be signed. So narrow the name to the scenario's own data, as the payload properties above do, since other scenarios' webhooks reach the same WireMock.

## Authorize requests with a token

Register a token under a name, then authorize requests with it:

```gherkin
Given the shop token with the following properties:
  | key        | ${env:SHOP_TOKEN_KEY} |
  | claim.shop | hawthorn-home         |
And a GET request to /api/shops/hawthorn-home/parcels
And the request is authorized with the shop token
```

A token is one of two kinds:

- **A JSON Web Token axx signs,** with rows:
  - `algorithm`: `HS256` (the default), `HS384`, `HS512`, `RS256` or `ES256`.
  - `key`: the shared secret, or for RS and ES, the PEM private key as a file of the project.
  - `key id`: the `kid` in the token's header.
  - `claim.<name>`: a claim, as a JSON value (`true`, `42`, `["parcels:read"]`) or text.
  - `expires in`: how long it lasts, `5m` unless it says otherwise.

  It is signed each time it is used, with `iat` and `exp` from then.
- **An OAuth 2.0 client credentials token axx gets from a token endpoint,** with rows `token url`, `client id`, `client secret`, and optionally `scope` and `audience`. It is got once for the run, and again once it expires:

  ```gherkin
  Given the shop-system token with the following properties:
    | token url     | http://localhost:8400/oauth/token |
    | client id     | wisteria-way                      |
    | client secret | ${env:WISTERIA_CLIENT_SECRET}     |
  ```

The request step sends `Authorization: Bearer <token>`. In other packs' property rows, `${token:<name>}` stands for the token, as in a WebSocket's header:

```gherkin
Given the tracking websocket with the following properties:
  | url                  | ws://localhost:8400/portal/track/PX-LIV-5701/live |
  | header.Authorization | Bearer ${token:shop}                             |
```

It works in the rows of what a scenario registers for itself (the `websocket`, `sse`, `grpc`, `jsonrpc` and `graphql` packs), and in the `cli` pack's command, arguments, input and environment. It doesn't work in the rows of message brokers, whose connections are opened for the whole run before any scenario registers a token.

## Keep secrets secret

Signing keys, client secrets and tokens are masked in logs and failures, whether they come from `${env:..}` or are written in the table. Keep real keys out of feature files anyway: `${env:..}` reads them from the environment, which CI fills from its secrets.

See the [rest](/references/packs/rest/) and [mock](/references/packs/mock/) packs' references for every step.
