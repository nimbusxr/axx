# axx-mailpit

[Mailpit](https://mailpit.axllent.org), the mail server that catches what services send in
tests, with **chaos rules**: a rule refuses the mail of the senders or recipients it matches,
with the SMTP code it gives, every time, until it is deleted. Mailpit's own chaos triggers fail a
share of *all* mail; a rule fails only the mail it names, so tests that each refuse their own
mail can run side by side.

The image is Mailpit built from its pinned source with [`chaos-rules.patch`](chaos-rules.patch),
which adds the rules, their API and their tests. Everything else is Mailpit.

```sh
docker run -p 8025:8025 -p 1025:1025 ghcr.io/nimbusxr/axx-mailpit
```

## The rules API

| Request | Does |
| --- | --- |
| `POST /api/v1/chaos/rules` `{"Stage": "recipient", "Match": "*@hawthorn-home.example", "ErrorCode": 451}` | adds a rule and answers it with its `ID` |
| `GET /api/v1/chaos/rules` | lists the rules, oldest first |
| `DELETE /api/v1/chaos/rules/{ID}` | deletes a rule |

- `Stage` is `sender` (the `MAIL FROM`) or `recipient` (each `RCPT TO`).
- `Match` is an address, or a pattern where `*` stands for any text; case does not matter.
- `ErrorCode` is from 400 to 599. When several rules match, the oldest one answers.

In axx, the mail pack's steps add and delete the rules
(`the {word} mailbox refuses mail to {string} with code {int}`); a rule lasts until its scenario
ends.

## Building and testing

```sh
docker build -t axx-mailpit:dev .   # applies the patch and runs its Go tests
./smoke-test.sh axx-mailpit:dev
```

To move to a newer Mailpit, set `MAILPIT_VERSION` and `MAILPIT_SHA256` (the source archive's)
in the Dockerfile, and check that the patch still applies.

Mailpit is MIT-licensed, © Ralph Slooten; the patch and this image are Apache-2.0, © NimbusXR.
