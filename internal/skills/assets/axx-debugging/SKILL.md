---
name: axx-debugging
description: Diagnose failing axx acceptance-test runs - failed assertions, undefined or ambiguous steps, OpenAPI validation errors, services that do not start, failed web steps, timeouts and flaky parallel scenarios. Use when `axx run` exits non-zero.
license: Apache-2.0
---

# Debugging axx failures

## Start from the exit code

| Code | Meaning | First move |
|---|---|---|
| 1 | a scenario failed | Read the step's expected and actual values |
| 2 | usage or configuration | Read the hint; `axx doctor`. Configuration errors point at `axx.yaml:line:col` |
| 3 | undefined or ambiguous step, or `axx lint` violations | `axx validate`, then `axx explain "<line>"`; a lint finding names the rule, the value and every `file:line` |
| 4 | a service did not start | The error has the service's last lines; all of it is in `.axx/logs/services.log` |

Every error has a code, like `AXX-E0408`: `axx explain AXX-E0408` says what it means and what to do.

## Get the facts first

- `axx run --json` gives each failure's `error.kind`, `expected`, `actual`, `logs`, `context` (the last HTTP exchange, say) and `rerun`. With the MCP tools, `failure_context` gives the same for the latest `scenarios_run`.
- Rerun one scenario: `axx run features/x.feature:LINE`.
- `axx explain "<step line>"` shows which step a line matches and what it captures.

## Common causes

- **Undefined step.** Use the `did you mean` suggestion or `axx steps search`; never change your text by guessing. A step of a pack the project lacks is undefined too, and axx names that pack: `axx pack add <name>`.
- **Ambiguous step.** Two steps match: make the text more specific, like `... on <service>`.
- **Expected X, got Y.** First check the data is the scenario's own: a scenario running at the same time may use the same ids. Then the value's type: `'5'` is the number 5, `'"5"'` the string.
- **OpenAPI validation errors on "the request is executed".** The request or response breaks the contract; the message names the rule (`validation.response.body.schema.required`). Fix the payload, or for a deliberately invalid request, set that rule to `IGNORE` with `the OpenAPI validation levels are:`.
- **Kafka check finds no records.** Leave `consumer.auto.offset.reset` out (earliest): `latest` reads only what comes after the check starts. Avro values need the topic client's deserializer and registry URL.
- **Timeouts.** For asynchronous behavior use the waiting steps (`within 5s ...`), not sleeps.
- **Passes alone, fails in a full run.** Shared data: give the scenario values of its own (`axx lint` finds seed and fixture values in more than one file). If it really must run alone, tag it with a `run.exclusive` tag.
- **Service never ready.** Check `services.<name>.ready` (URL, port, timeout); `axx up` and fetch the health URL yourself; read `.axx/logs/services.log`.
- **Fixture errors (`AXX-E09xx`).** `axx fixtures check` reports drift: change the fixture or factory and `axx fixtures generate`. `axx fixtures explain <file> <path>` names what sets a generated value. The `axx-test-data` skill covers factories.
- **A web step failed.** The failure's `context` names each web app's page; the failed step attaches a screenshot, and the logs name the Playwright trace in `.axx/web/traces`. `steps_try` in `axx mcp`, then `web_page`, shows the page live.
- **Services from an axx host (`AXX-E05xx`).** When a sandbox keeps you from Docker, the person runs `axx host` and `axx run` asks it for the services. A run waiting "for the person running the axx host to approve it" changed `axx.yaml`'s services or a compose file: tell them what and why. `AXX-E0507`: not answered yet, run again later. `AXX-E0501`: refused, change it back or ask them. Never edit `.axx/host.json` or start the services another way.

## Don't

- Don't loosen an assertion to make a test pass: find out whether the product is wrong first.
- Don't add fixed sleeps, or edit generated files (`*.generated.yaml`, fixture outputs): regenerate them.
