---
title: Send REST requests
description: Payloads from files, the redirects the REST pack follows and the ones it leaves to the scenario, cookies, and the headers custom steps give a request.
---

A scenario registers a REST service, adds requests to it, executes them and checks their responses ([rest pack](/references/packs/rest/)). This page is about what happens in between: the payload a request sends, the redirects it follows, the cookies it sends, and the headers a custom step gives it.

The REST pack tests APIs. To test a web app's pages, forms or sign-in, use the web-core pack: a real browser keeps cookies, follows redirects and submits forms, as the app's users do ([Test web apps](/guides/test-web-apps/)).

## Payloads from files

A payload starts from an example of the service's OpenAPI specification, from an empty template the payload property steps fill, or from a file of the project, sent as it is:

```gherkin
Scenario Outline: A shop's order system registers the parcels it exports
  Given a POST request to /api/parcels
  And a request payload using an application/json requests/<order>.json resource
  When the request is executed
  Then the response status code is 201
  And the response header Location is '/api/parcels/<reference>'

  Examples:
    | order      | reference   |
    | order-7731 | PX-REG-1301 |
    | order-7732 | PX-REG-1302 |
```

- **The file is found like the other files steps name:** relative to the `resources` directories, or to `axx.yaml`'s directory.
- **A JSON payload can still change.** The payload property steps edit it, like a payload from an example.
- **A form payload file is a JSON object.** With `application/x-www-form-urlencoded`, its properties are sent form-encoded.
- **No OpenAPI specification is needed,** which suits a callback or a webhook that the service has no contract for.

A [fixture factory](/guides/fixture-factories/) keeps such files in one place and checks them against a schema. The parcels example generates the two files above with `requests/registrations.factory.yaml`, from the `ParcelRequest` schema of the service's OpenAPI contract.

## Redirects

| Request | Redirects it follows | The response the steps check |
|---|---|---|
| `GET`, `HEAD` | 301, 302, 303, 307 and 308 | the one the redirects lead to |
| `POST`, `PUT`, `PATCH`, `DELETE`, ... | `303 See Other`, with a `GET` | after a 303, the one it leads to |
| `POST`, `PUT`, `PATCH`, `DELETE`, ... | none of the others: 301, 302, 307 and 308 | the redirect itself |

A redirect the pack follows is not kept: its status, its `Location` header and the cookies it sets are not the response's, and the response steps cannot check them. The scenario's log shows the request as the step sent it and the status at the end, like `GET http://localhost:8080/api/parcels/PX-REG-1010 -> 200 OK (12ms, 412 bytes)`.

The `GET` that follows a 303 has no body and no `Content-Type`. It has the request's other headers, `Authorization` and `Cookie` only when the redirect stays on the same host or goes to a subdomain of it.

### Redirects left to the scenario

A `POST`, `PUT`, `PATCH` or `DELETE` answered with 301, 302, 307 or 308 has the redirect as its response. Check its status and its `Location` header, and send the request it leads to as the next ordered request. Say a service moved its API from `/api/v1` and answers the old paths with `308 Permanent Redirect`:

```gherkin
Scenario: The old API sends shops to where parcels are cancelled now
  Given a 1st ordered DELETE request to /api/v1/parcels/PX-REG-1010
  And a 2nd ordered DELETE request to /api/parcels/PX-REG-1010
  When the 1st ordered request is executed
  Then the 1st ordered response status code is 308
  And the response header Location is '/api/parcels/PX-REG-1010' for 1st ordered response
  When the 2nd ordered request is executed
  Then the 2nd ordered response status code is 204
```

## Cookies

The REST pack keeps no cookies. A `Set-Cookie` response header is never sent back: not with the scenario's later requests, and not to where a redirect leads. A request carries the cookies its scenario gives it, in its `Cookie` header (`the request header Cookie is '...'`), and nothing else, so scenarios running side by side never share cookies.

## Headers from custom steps

A custom step changes a request the pack's steps built, before it is executed, through the request's headers: `Header()` returns the headers it is sent with, and `Set`, `Add` and `Del` change them. `SetHeader(name, value)` does what the request header steps do. [Write custom steps](/guides/write-custom-steps/#change-a-rest-request) has an example.
