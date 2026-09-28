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
| by **SMTP**, to a relay or a provider's SMTP endpoint | a mail server that catches it: [smtp4dev](https://github.com/rnwood/smtp4dev), which can also stub refusals, or [Mailpit](https://mailpit.axllent.org), GreenMail, Inbucket | a mailbox with an `imap://` or `pop3://` `url`, or Mailpit's, read through its API |
| by SMTP, and it must really arrive | a real inbox, in a staging environment | a mailbox with an `imaps://` `url` |
| through a **provider's HTTP API** (SendGrid, SES, Mailgun, Postmark, Resend) | WireMock, against the provider's contract | the [mock pack](/guides/mock-dependencies/): the request's payload properties |

Point your service's SMTP settings at the mail server of the test environment, as the parcels example does in its Compose file:

```yaml title="compose.yaml"
services:
  mail:
    image: rnwood/smtp4dev:3.15.0
    ports: ['1025:25', '1143:143', '8025:80'] # SMTP, IMAP, its web interface
  app:
    environment:
      PARCELS_SMTP_ADDR: mail:25
```

## Register the mailbox

```gherkin
Background:
  Given the shops mailbox with the following properties:
    | url      | imap://${sys:local.host}:1143 |
    | username | axx                           |
    | password | axx                           |
```

- **`url`:** Mailpit's address (`http://` or `https://`), or `pop3://`, `pop3s://`, `imap://` or `imaps://` and the mail server's host.
- **`username` and `password`:** sign in over POP3 and IMAP, or to a Mailpit behind a password. The password is masked.
- **`folder`:** the IMAP folder, `INBOX` unless it says otherwise.

smtp4dev takes any username and password over IMAP, and its `INBOX` has every email it received. Mailpit answers POP3 once it is given a password file (`--pop3-auth-file`, or `MP_POP3_AUTH`).

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

## Refuse mail

What does your service do when a mail server refuses its email? Stub the refusal in the mail server, as you stub an HTTP dependency's failures in WireMock's mappings: by the data a scenario uses. smtp4dev runs a JavaScript expression for each recipient, and `error(code, message)` refuses it with that SMTP code:

```yaml title="compose.yaml"
services:
  mail:
    image: rnwood/smtp4dev:3.15.0
    environment:
      # quince-and-quill's mail server is busy: it asks to be tried again later.
      ServerOptions__RecipientValidationExpression: >-
        recipient.toLowerCase().endsWith("@quince-and-quill.example")
        ? error(451, "Mailbox busy, try again later") : true
```

A scenario whose shop is quince-and-quill then sees how your service copes:

```gherkin
Scenario: A registration email the shop's mail server refuses does not stop the registration
  Given a seeds/mail-refused.yaml db seed
  And a POST request to /api/parcels
  And a request payload using an application/json content example
  And the request payload property sender is 'quince-and-quill'
  When the request is executed
  Then the response status code is 201
```

- A 4xx code, like 451, is a failure the sender should retry; a 5xx, like 550, one it should not.
- The stub refuses only the recipients it names, so scenarios running beside it are not affected: give each refusal a recipient of its own.
- `CommandValidationExpression` refuses at other stages, such as the sender's `mail from:` (smtp4dev passes the command's verb in lowercase), and `MessageValidationExpression` refuses whole messages.

## Keep scenarios apart

A check only looks at the emails that arrived since its scenario started, and axx never deletes or marks what it reads. Scenarios running in parallel share the mailbox, so each checks its own mail by a recipient or a subject unique to it, such as a shop or a parcel reference of its own.

For a run, axx reads each mailbox the checks name from when the apps are up:
- **Mailpit and IMAP** say when each email arrived.
- **POP3** doesn't, so axx takes what the mailbox held when it started reading as old.

See the [mail pack's reference](/references/packs/mail/) for every step.
