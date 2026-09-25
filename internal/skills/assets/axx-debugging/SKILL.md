---
name: axx-debugging
description: Diagnose failing axx acceptance-test runs (Gherkin scenarios run by the axx CLI) - failed assertions, undefined or ambiguous steps, OpenAPI validation errors, apps that do not start, timeouts and flaky parallel scenarios. Use when `axx run` exits non-zero.
license: Apache-2.0
---

# Debugging axx failures

## Start from the exit code

| Code | Meaning | First move |
|---|---|---|
| 1 | a scenario failed | Read the step's expected/actual values below |
| 2 | usage or config error | Read the `hint`. Run `axx doctor`. Config errors point at `axx.yaml:line:col` |
| 3 | undefined or ambiguous step, or `axx lint` violations | `axx validate`, then `axx explain "<line>"`. For lint, each finding names the rule, the colliding value and every `file:line` |
| 4 | an app did not start | The error includes the last lines of the app's output. Full logs are in `.axx/logs/apps.log` |
| 130 | interrupted | Nothing ran to completion. Rerun |

Every error also carries a code such as `AXX-E0408`. Run `axx explain AXX-E0408` to see what it means and how to fix it, or look it up in `references/error-codes.md`.

## Get the facts before changing anything

- `axx run --json` returns a structured report. Each failure has `error.kind`: assertion, error, timeout, panic, undefined, ambiguous or pending. It also carries `expected`, `actual`, `logs`, `context` (for example the last HTTP request and response) and a `rerun` command.
- Rerun one scenario with the `rerun` command: `axx run features/x.feature:LINE`. The line of any step inside the scenario also works.
- `axx explain "<step line>"` shows which definition a line matches and what it captures.

## Common causes

- **Undefined step.** The text differs from every definition. Use the `did you mean` suggestion or `axx steps search`, and never rewrite a step definition to match your text. A step from a pack the project doesn't list is undefined too: add the pack with `axx pack add <name>` (`axx pack list` shows axx's packs). Check for extra spaces, singular/plural (`time(s)` is optional), `a` vs `an`, and quotes around `{string}` values.
- **Ambiguous step.** Two definitions match. Make the text more specific, for example by adding `on <service>`.
- **Assertion expected X, got Y.** Check whether the test data is unique: another scenario running in parallel may have changed or used the same ids. Then check whether the value's type is wrong (`'5'` is the number 5; use `'"5"'` for the string `"5"`).
- **OpenAPI validation errors on "the request is executed".** The request or the response violates the spec. The message names the rule, e.g. `validation.response.body.schema.required`. Fix the payload, or (for deliberate negative tests) relax that rule for the scenario: `Given the OpenAPI validation levels are:` with `| <rule> | IGNORE |`.
- **Timeouts.** A step exceeded `run.timeouts.step`. For asynchronous behavior, use the polling steps (`within 5s a selection of at least 1 row ...`) instead of adding sleeps.
- **Passes alone, fails in a full run.** Shared data or shared state. Give the scenario unique data (`axx lint` finds seed and fixture values used by more than one file). If it genuinely must run alone, tag it `@isolated`, or whatever is listed in `run.exclusive`.
- **Fixture errors (`AXX-E09xx`).** `axx fixtures check` reports drift when a generated file no longer matches its factory spec: edit the spec and run `axx fixtures generate`. Generate refuses (`AXX-E0903`) to overwrite a generated file someone edited by hand; move the change into the spec, or revert the file. Messages name the factory, the fixture and the field.
- **App never becomes ready.** Check `apps.<name>.ready` (URL, port, timeout). `axx up` then `curl` the health URL yourself. Read `.axx/logs/apps.log`.

## Don't

- Don't loosen an assertion just to make a test pass. Find out whether the product is wrong first.
- Don't add fixed sleeps. Use readiness checks and polling steps.
- Don't edit generated files (`*.generated.yaml`, fixture outputs). Regenerate them instead.
