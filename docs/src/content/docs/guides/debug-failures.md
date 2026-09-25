---
title: Debug failures
description: Go from a red axx run to the cause - read the exit code, rerun one scenario, explain a step, read expected and actual values, find app logs, and fix flaky parallel scenarios.
---

## Start from the exit code

| Code | What happened | First move |
| --- | --- | --- |
| `1` | a scenario failed | read the failing step's expected and actual values |
| `2` | usage or configuration error | read the `hint`; config errors point at `axx.yaml:line:col`; run `axx doctor` |
| `3` | undefined or ambiguous step | `axx validate`, then `axx explain "<line>"` |
| `4` | an app did not start or stop | the error shows the app's last output lines; the full log is `.axx/logs/apps.log` |
| `130` | interrupted | nothing completed; run again |

Every error also has a stable code (`AXX-E0408`, say) that links to its entry in the [error code reference](/references/error-codes/).

## Read the failure

```console
$ axx run
...
  Scenario: The service says hello  # features/smoke.feature:7
    ✓ Given the hello-axx service with the following properties:  (background)
        | url | http://${sys:local.host}:8000 |
    ✓ Given a GET request to /hello.json
    ✓ When the request is executed
        log: GET http://localhost:8000/hello.json -> 200 OK (2ms, 26 bytes)
        attachment: response body (application/json, 26 B)
          {"message": "Hello, axx"}
    ✓ Then the response status code is 200
    ✗ And the response payload property message is 'Hello, world'
        Response payload property message is not Hello, world
          expected: "Hello, world"
          actual:   "Hello, axx"

Failed scenarios:
  ✗ The service says hello  # features/smoke.feature:7
      rerun: axx run features/smoke.feature:7
```

Each failure names the step, the expected and actual values, and the command that reruns only that scenario. Steps that talk to a service attach what they sent and received. For the same failure as data, with the last request and response, use `axx run --json` ([the run report](/references/json-output/#the-run-report)).

## Rerun one scenario

```sh
axx up                                          # keep the apps running while you iterate
axx run features/register-parcels.feature:15    # the scenario on (or containing) line 15
```

The line of any step inside the scenario works too.

## Undefined and ambiguous steps

```console
$ axx explain 'the mocked request named postcode-check was received 2 times'
undefined: no step matches
did you mean:
  the mocked request named {word} was received exactly {int} time(s)  (mock.count.exactly)
  the mocked request named {word} was received at least {int} time(s)  (mock.count.atLeast)
  the mocked request named {word} was received at most {int} time(s)  (mock.count.atMost)
  ...
search all steps with `axx steps search <words>`
```

- **Undefined**: the text differs from every step. Use the suggestion or `axx steps search`. Look for extra spaces, `a` versus `an`, singular versus plural, and missing quotes around `{string}` values. Never change a step definition to match your text.
- **Ambiguous**: two steps match. Make the line more specific, usually by naming the service with `on <service>`.

## Common causes

- **Expected X, got Y, and the product looks right.** Another scenario running at the same time may have used the same ids. Check that the data is unique (`axx lint`), then check the value's type: the step's entry in the [step reference](/references/steps/) says how it reads values (`'5'` and `'"5"'` differ).
- **OpenAPI validation error on `the request is executed`.** The request or the response violates the document; the message names the rule, such as `validation.response.body.schema.required`. Fix the payload or the service. For a deliberate negative test, relax that rule in the scenario ([Validate against OpenAPI](/guides/validate-openapi/#validation-levels)).
- **Timeout.** A step exceeded `run.timeouts.step`. For asynchronous behavior use a polling step (`within 10s a selection of at least 1 row ...`) instead of a sleep or a longer timeout.
- **Passes alone, fails in the full run.** Shared data or shared state. Make the data unique; if the scenario really must run alone, tag it for `run.exclusive` ([Run in parallel](/guides/parallel-runs/)).
- **The app never becomes ready.** Check `apps.<name>.ready` (URL, port, timeout). Run `axx up`, then `curl` the health URL yourself, and read `.axx/logs/apps.log`.

## Debug the service itself

Set a breakpoint in your service under test and run the scenario against it:

```sh
axx run --attach parcels features/register-parcels.feature:15   # you start parcels from your IDE; axx waits for it
axx run --debug=parcels features/register-parcels.feature:15    # axx starts parcels with its debug command
```

[Manage the app lifecycle](/guides/manage-app-lifecycle/#debug-an-app) shows how to configure `apps.<name>.debug`.

## Stop in step code

To see exactly what a step does, why it fails, or what your service sent back, set breakpoints in the step's own Go code, whether it comes from one of Axx's packs or from your own, and step through it:

```sh
axx run --debug-steps features/register-parcels.feature:15
```

Axx builds itself with debug information (the first time; later runs reuse the build), starts under [Delve](https://github.com/go-delve/delve), Go's debugger, and waits for a debugger on port 2345 (`--debug-steps=<port>` picks another). The scenario starts when one attaches. Step timeouts are off while you debug, and the exit code is the run's as usual.

- **GoLand, or IntelliJ IDEA with the Go plugin:** click the gutter icon of a scenario and choose *Debug*. The axx plugin attaches the Go debugger for you. From a terminal run, start the *Debugger: axx-steps* configuration (written by `axx ide intellij`).
- **VS Code with the Go extension:** use the debug button of a scenario in the gutter or the Testing view. To attach to a run you started in a terminal, use *axx: attach to steps* (written by `axx ide vscode`).
- **Anything else:** `dlv connect 127.0.0.1:2345`.

Jump from a step in a feature file to its code with go-to-definition ([Set up your editor](/guides/set-up-your-editor/)). Steps of Axx's packs open in axx's source, which the debug build is compiled from, so breakpoints set there hold.

It needs Go, to build, and Delve: `go install github.com/go-delve/delve/cmd/dlv@latest`.

## Logs

| File | Contents |
| --- | --- |
| `.axx/logs/apps.log` | output of every app Axx started |

Add `-v` or `-vv` to any command for more detail from Axx itself.

## Don't

- Don't loosen an assertion to make a test pass. Find out whether the product is wrong first.
- Don't add fixed sleeps. Use readiness checks and polling steps.
- Don't edit generated files (`axx-lint.generated.yaml`, generated fixtures); change their source and regenerate.
