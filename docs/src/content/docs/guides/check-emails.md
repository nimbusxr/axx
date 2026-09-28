---
title: Check emails
description: Check the emails your services send, by their recipients, subject, text, HTML, attachments and headers, read from Mailpit or any mailbox over POP3 or IMAP; and the mail they send through a provider's API.
---

Services email people: a label to a shop, a delivery notice, a password reset. Your service sends its mail as it does in production, by SMTP, to the mail server of your test environment, and the `mail` pack reads the mailbox that catches it.

```gherkin
Scenario: A shop gets its parcel's label by email
  Given a seeds/mail-registered.yaml db seed
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload property reference is 'PX-MAIL-9701'
  And the request payload property sender is 'juniper-and-jay'
  When the request is executed
  Then the response status code is 201
  And within 10s the shops mailbox has an email where:
    | to         | orders@juniper-and-jay.example |
    | subject    | Parcel PX-MAIL-9701 registered |
    | attachment | PX-MAIL-9701-label.zpl         |
```

Add the pack to the project with `axx pack add mail` ([Choose packs](/guides/use-packs/)).

## Which mailbox

How your service sends its mail decides where a test finds it:

| Your service sends | In the tests | The check |
| --- | --- | --- |
| by **SMTP**, to a relay or a provider's SMTP endpoint | a mail server that catches it: [Mailpit](https://mailpit.axllent.org), the WireMock of mail | a mailbox with Mailpit's `url`, read through its API |
| by SMTP, to a catcher your team already runs | GreenMail, smtp4dev, Inbucket | a mailbox with a `pop3://` or `imap://` `url` |
| by SMTP, and it must really arrive | a real inbox, in a staging environment | a mailbox with an `imaps://` `url` |
| through a **provider's HTTP API** (SendGrid, SES, Mailgun, Postmark, Resend) | WireMock, against the provider's contract | the [mock pack](/guides/mock-dependencies/): the request's payload properties |

Point your service's SMTP settings at the mail server of the test environment, as the parcels example does in its Compose file:

```yaml title="compose.yaml"
services:
  mailpit:
    image: axllent/mailpit:v1.31.3
    ports: ['8025:8025', '1025:1025']
  app:
    environment:
      PARCELS_SMTP_ADDR: mailpit:1025
```

## Register the mailbox

```gherkin
Background:
  Given the shops mailbox with the following properties:
    | url | http://${sys:local.host}:8025 |
```

- **`url`:** Mailpit's address (`http://` or `https://`), or `pop3://`, `pop3s://`, `imap://` or `imaps://` and the mail server's host.
- **`username` and `password`:** sign in over POP3 and IMAP, or to a Mailpit behind a password. The password is masked.
- **`folder`:** the IMAP folder, `INBOX` unless it says otherwise.

Mailpit answers POP3 once it is given a password file (`--pop3-auth-file`, or `MP_POP3_AUTH`).

## Check an email

```gherkin
Then within 10s the shops mailbox has an email where:
  | to      | orders@larkspur-lane.example                 |
  | subject | Parcel PX-MAIL-9702 delivered                |
  | html    | <b>PX-MAIL-9702</b> was delivered in Leipzig |
```

| Row | What it checks |
| --- | --- |
| `to`, `cc` | an address the email went to, or was copied to |
| `from` | the address it came from |
| `subject` | its subject, all of it |
| `text` | text its plain-text body contains |
| `html` | text its HTML body contains, tags and all |
| `attachment` | the file name of an attachment |
| `header <name>` | a header's value |

Addresses compare without regard to case, and `text` and `html` take runs of spaces and line breaks as one space. Subjects and names in other charsets are decoded first. The check waits for an email that meets every row: 10 seconds, or the time `within` gives. When none does, it lists the emails that arrived.

## Keep scenarios apart

A check only looks at the emails that arrived since its scenario started, and axx never deletes or marks what it reads. Scenarios running in parallel share the mailbox, so each checks its own mail by a recipient or a subject unique to it, such as a shop or a parcel reference of its own.

For a run, axx reads each mailbox the checks name from when the apps are up:
- **Mailpit and IMAP** say when each email arrived.
- **POP3** doesn't, so axx takes what the mailbox held when it started reading as old.

See the [mail pack's reference](/references/packs/mail/) for every step.
