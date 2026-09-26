---
title: Sign in to web apps
description: Sign in the way people do, through every page of your identity provider, or start scenarios with a session - cookies, a token, storage or HTTP credentials - and keep passwords and tokens out of reports and traces.
---

Every scenario has a browser context of its own, a fresh browser profile, so each one signs in: nothing a scenario signs in to is shared with another, which is what lets them run side by side ([How the web-core pack works](/explanations/web-browsers/#isolation)). There are two ways.

## Sign in as people do

Signing in is pages, fields and buttons like any others, however many pages it takes: the browser follows your app to its identity provider and back, as it does for people. A `Background` does it for all the scenarios of a feature:

```gherkin
Background:
  Given the parcels web app with the following properties:
    | url | http://localhost:8400/portal |
  And the "/account" page is opened
  And the "Sign in" link is clicked
  And the "Log in with Email" button is clicked
  And the "Email Address" field is filled with "orders@fjord-outdoor.example"
  And the "Password" field is filled with "${env:FJORD_OUTDOOR_PASSWORD}"
  And the "Login" button is clicked
  And the "Grant Access" button is clicked
  And the page shows "Signed in as Fjord Outdoor"
```

This is the sign-in your shops go through, so it is checked in every scenario, and against a provider in your Compose file it takes about a second.

## Start with a session

When a scenario is about what a signed-in shop does rather than about signing in, start its browser signed in, with what your app keeps its session in. Each is a property of the web app, and each goes to the app's own address only:

| Property | |
| --- | --- |
| `cookie.<name>` | a cookie, such as a session cookie: `cookie.session` |
| `header.<name>` | a header on every request to the app, such as a token: `header.Authorization` with `Bearer ${env:SHOP_TOKEN}` |
| `local storage.<key>` | an item of the page's local storage, where single-page apps often keep their tokens |
| `session storage.<key>` | an item of each tab's session storage, where some sign-in libraries keep theirs |
| `username`, `password` | HTTP authentication, for apps (or test environments) behind a login prompt |

```gherkin
Scenario: A returning shop sees its parcels
  Given the shop web app with the following properties:
    | url                 | http://localhost:8400/portal |
    | local storage.token | ${env:SHOP_TOKEN}            |
  When the "/parcels" page of the shop web app is opened
  Then the page shows "Signed in as Fjord Outdoor"
```

Where the token comes from is up to your test environment: a long-lived test token in the environment, one your identity provider issues to a test client, or a session your service creates for a seeded shop. Scenarios still share nothing: every scenario's browser starts with its own copy.

A step of your own can wrap either way in your own words, like `Given the shop is signed in as fjord-outdoor` ([Write custom steps](/guides/write-custom-steps/)).

## Keep secrets out of reports

Keep passwords and tokens in the environment, as `${env:…}`. Whatever a step takes from the environment is a secret: failure messages, the failure report and saved traces show it as `********`, down to the form data the browser sends and the headers of its requests. Screenshots and videos show the page as it is: a password field shows only dots, so type secrets into password fields.

## An identity provider next to your apps

For your own tests, run [Dex](https://dexidp.io), a small OpenID Connect provider, in the Compose file with shops of your choosing. Your app signs in with it as it does with your real provider:

```yaml title="compose.yaml"
services:
  dex:
    image: ghcr.io/dexidp/dex:v2.45.1
    command: ['dex', 'serve', '/etc/dex/config.yaml']
    volumes: ['./dex.yaml:/etc/dex/config.yaml:ro']
    ports: ['5556:5556']
  parcels:
    build: ../app
    ports: ['8400:8400']
    extra_hosts: ['dex.localhost:host-gateway'] # Dex, where the browsers find it
```

```yaml title="dex.yaml"
issuer: http://dex.localhost:5556/dex
storage:
  type: memory
web:
  http: 0.0.0.0:5556
staticClients:
  - id: parcels-portal
    name: Parcels portal
    secret: portal-client-secret
    redirectURIs: ['http://localhost:8400/portal/callback']
enablePasswordDB: true
staticPasswords:
  - email: orders@fjord-outdoor.example
    # htpasswd -nbBC 10 "" "$FJORD_OUTDOOR_PASSWORD" | tr -d ':\n'
    hash: '<the bcrypt hash of the password>'
    username: Fjord Outdoor
    userID: fjord-outdoor
connectors:
  - type: mockCallback
    id: mock
    name: Example
```

- The `issuer` is one address for both the browser and your app. The browsers, on your machine, take any `*.localhost` name for your machine, where Dex's port is published; `extra_hosts` sends your app's container there too. Give your app that issuer, the client's `id` and `secret`, and its callback. An app that runs on your machine needs no `extra_hosts`.
- Dex asks which way to sign in (`Log in with Email` or `Log in with Example`), then for the email address and password, then to grant access. `oauth2: {skipApprovalScreen: true}` leaves out the last page.
- `Log in with Example` signs in a fixed test user, Kilgore Trout, without a password: enough for scenarios about what a signed-in shop sees rather than about signing in.
- Dex keeps no session of its own: after signing out of your app, signing in asks for the password again.
