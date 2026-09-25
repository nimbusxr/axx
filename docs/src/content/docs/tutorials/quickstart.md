---
title: Quickstart
description: Install Axx and run your first passing scenario against a small web server on your machine, in about ten minutes.
sidebar:
  order: 1
---

In this tutorial, we'll install Axx, create a project, and write a scenario that tests a small web server running on your machine. We'll run it and watch it pass, then break it on purpose to see what a failure looks like.

It takes about ten minutes. You'll need a terminal, [Go](https://go.dev/dl/) 1.27 or newer to install Axx, and Python 3, which we'll use as the web server.

## Install Axx

Install Axx with Go:

```sh
go install github.com/nimbusxr/axx/cmd/axx@latest
```

Check that your shell finds it:

```sh
axx version
```

Axx prints its version. If your shell says `command not found`, add Go's `bin` directory to your `PATH` (`export PATH="$PATH:$(go env GOPATH)/bin"`) and try again.

## Create a project

Make a directory for the project and let Axx set it up:

```sh
mkdir hello-axx
cd hello-axx
axx init
```

```console
  create  axx.yaml
  create  axx-packs.yaml
  create  features/smoke.feature
  create  .github/workflows/acceptance.yml
  create  AGENTS.md
  create  .gitignore

next: edit apps in axx.yaml, then `axx doctor` and `axx run`
```

Axx created a configuration file, `axx.yaml`, and a first feature file, `features/smoke.feature`. We'll change both. It also created `axx-packs.yaml`, the list of the packs of steps the project uses. It lists `rest`, whose steps send HTTP requests, and that's all we need. We won't need the other files in this tutorial.

## Give the web server something to serve

Our web server will serve the files in this directory. Create a small JSON file for it:

```sh
echo '{"message": "Hello, axx"}' > hello.json
```

## Tell Axx how to start the server

Open `axx.yaml`. At the bottom, replace the whole `apps:` section with this one:

```yaml title="axx.yaml"
apps:
  hello-axx:
    command: python3 -m http.server 8000
    ready:
      http:
        url: http://${sys:local.host}:8000/
```

Axx now knows how to start the server, and how to tell when it's ready: when `http://localhost:8000/` answers.

## Write the scenario

Replace everything in `features/smoke.feature` with:

```gherkin title="features/smoke.feature"
Feature: Hello axx

  Background:
    Given the hello-axx service with the following properties:
      | url | http://${sys:local.host}:8000 |

  Scenario: The service says hello
    Given a GET request to /hello.json
    When the request is executed
    Then the response status code is 200
    And the response payload property message is 'Hello, axx'
```

Every line is a step Axx already knows. The `Background` registers the server as a service named `hello-axx`. The scenario sends it a request and checks the answer.

## Check the scenario

Before running anything, ask Axx to check every line:

```sh
axx validate
```

```console
axx: preparing rest (once; cached for later runs)
axx: ready in 31s
1 file, 1 scenario, 5 steps: ok
```

Axx matched each line to one of its steps, without starting the server. The first time, it also prepared itself with the project's packs. It won't need to do that again.

## Run it

```sh
axx run
```

```console
axx: starting hello-axx (logs: .axx/logs/apps.log)
Feature: Hello axx

  Scenario: The service says hello  # features/smoke.feature:7
    ✓ Given the hello-axx service with the following properties:  (background)
        | url | http://${sys:local.host}:8000 |
    ✓ Given a GET request to /hello.json
    ✓ When the request is executed
        log: GET http://localhost:8000/hello.json -> 200 OK (4ms, 26 bytes)
        attachment: response body (application/json, 26 B)
          {"message": "Hello, axx"}
    ✓ Then the response status code is 200
    ✓ And the response payload property message is 'Hello, axx'

1 scenario (1 passed)
5 steps (5 passed)
Finished in 4ms
```

Notice what happened. Axx started the web server, waited until it answered, ran the scenario, and stopped the server again. Under the request step, the `log:` line shows the request Axx sent, and the attachment shows the response it got back.

## Break it on purpose

Change the last line of `features/smoke.feature` so it expects a different message:

```gherkin
    And the response payload property message is 'Hello, world'
```

Run it again:

```sh
axx run
```

```console
axx: starting hello-axx (logs: .axx/logs/apps.log)
Feature: Hello axx

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

1 scenario (1 failed)
5 steps (1 failed, 4 passed)
Finished in 3ms
```

Axx marks the step that failed and shows what it expected next to what it got. It also prints a command that reruns only this scenario.

Change the line back to `'Hello, axx'` and run `axx run` once more. The scenario passes again.

## What you've done

You installed Axx, told it how to start a service, wrote a scenario, ran it, and read a failure. That's the loop you'll use with every Axx suite.

Next, in [Your first suite](/tutorials/first-suite/), we'll test a real service with a database and a dependency it calls.
