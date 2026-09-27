---
title: Send REST requests
description: The redirects the REST pack follows and the ones it leaves to the scenario, cookies and sessions, and the headers custom steps give a request.
---

A scenario registers a REST service, adds requests to it, executes them and checks their responses ([rest pack](/references/packs/rest/)). This page is about what happens in between: the redirects a request follows, the cookies it sends, and the headers a custom step gives it.

## Redirects

| Request | Redirects it follows | The response the steps check |
|---|---|---|
| `GET`, `HEAD` | 301, 302, 303, 307 and 308 | the one the redirects lead to |
| `POST`, `PUT`, `PATCH`, `DELETE`, ... | `303 See Other`, with a `GET` | after a 303, the one it leads to |
| `POST`, `PUT`, `PATCH`, `DELETE`, ... | none of the others: 301, 302, 307 and 308 | the redirect itself |

A redirect the pack follows is not kept: its status, its `Location` header and the cookies it sets are not the response's, and the response steps cannot check them. The scenario's log shows the request as the step sent it and the status at the end, like `POST http://localhost:8400/portal/parcels -> 200 OK (38ms, 4816 bytes)`.

### A form answered with 303 See Other

A web app answers a form it accepts with `303 See Other` to the page that shows the result, so that reloading that page does not send the form again. A browser follows it with a `GET`, and so does the REST pack: the response is that page. The parcels example registers a parcel with its shop portal's form (`features/portal-forms.feature`):

```gherkin
Scenario: Registering a parcel with the form leads to the parcel's page
  Given the portal service with the following properties:
    | url | http://localhost:8400/portal |
  And a POST request to /parcels
  And a request payload using an application/x-www-form-urlencoded empty content template
  And the request payload properties are:
    | sender    | alder-stationery     |
    | reference | PX-FORM-1001         |
    | weight    | 1200                 |
    | service   | STANDARD             |
    | name      | Ada Lovelace         |
    | street    | Invalidenstrasse 116 |
    | postcode  | "10115"              |
    | city      | Berlin               |
    | country   | DE                   |
  When the request is executed
  Then the response status code is 200
  And the response header Content-Type is 'text/html; charset=utf-8'
  And the response body contains 'Parcel PX-FORM-1001 registered'
```

The `GET` that follows the 303 has no body and no `Content-Type`. It has the request's other headers, `Authorization` and `Cookie` only when the redirect stays on the same host or goes to a subdomain of it.

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

## Cookies and sessions

The REST pack keeps no cookies. A `Set-Cookie` response header is never sent back: not with the scenario's later requests, and not to the page a redirect leads to. A request carries the cookies its scenario gives it, in its `Cookie` header, and nothing else, so scenarios running side by side never see each other's sessions.

Give a request a cookie with its `Cookie` header. The parcels portal remembers a returning shop in its `shop` cookie:

```gherkin
Scenario: A returning shop sees the pickups it can plan
  Given the portal service with the following properties:
    | url | http://localhost:8400/portal |
  And a POST request to /parcels
  And a request payload using an application/x-www-form-urlencoded empty content template
  And the request payload properties are:
    | sender    | alder-stationery |
    | reference | PX-FORM-1002     |
    | weight    | 800              |
    | name      | Mary Somerville  |
    | postcode  | "80331"          |
    | city      | Munich           |
    | country   | DE               |
  And a 2nd ordered GET request to /parcels
  And the request header Cookie is 'shop=alder-stationery' for 2nd ordered request
  When the request is executed
  And the 2nd ordered request is executed
  Then the 2nd ordered response status code is 200
  And the response body contains 'Pickup PX-FORM-1002' for 2nd ordered response
```

The `Cookie` header goes along when the request follows a redirect on the same host or to a subdomain of it, and not to another host.

A session the service hands out when someone signs in, or a CSRF token it puts in a page, is new on every run, so a feature file cannot spell it out. A custom step reads it from the response it came in (`Exchange().Header`, `Exchange().Body`) and puts it on a later request ([Change a REST request](/guides/write-custom-steps/#change-a-rest-request)). The step finds both in the scenario's own requests: kept anywhere else, a session would reach the scenarios running beside it.

## Headers from custom steps

A custom step changes a request the pack's steps built, before it is executed, through the request's headers: `Header()` returns the headers it is sent with, and `Set`, `Add` and `Del` change them. `SetHeader(name, value)` does what the request header steps do. [Write custom steps](/guides/write-custom-steps/#change-a-rest-request) has an example.
