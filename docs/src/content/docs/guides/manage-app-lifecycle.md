---
title: Manage the app lifecycle
description: Declare how Axx starts, waits for, stops and cleans up the apps under test, keep them running with axx up, attach apps you run yourself, and start only the apps a run needs.
---

`apps:` in `axx.yaml` describes the system under test: your service and anything it needs, such as databases, brokers and mocks. `axx run` starts them, waits until they are ready, runs the scenarios, stops them and runs their cleanup.

## Declare an app

```yaml title="axx.yaml"
apps:
  infra:
    dir: ./infra                          # relative to axx.yaml
    command: docker compose up --wait postgres kafka
    ready:
      tcp: localhost:5432
    cleanup: docker compose down -v --remove-orphans

  api:
    dependsOn: [infra]
    command: ./gradlew bootRun            # or npm start, go run ./cmd/api, ...
    env:
      SPRING_PROFILES_ACTIVE: acceptance
    ready:
      http:
        url: http://localhost:8080/actuator/health
      timeout: 120s
      interval: 1s
    stop:
      signal: SIGTERM
      grace: 20s
```

Each key is described in the [configuration reference](/references/config/#apps). A command that exits `0` before the app is ready is fine, which is what `docker compose up -d` or `--wait` does: Axx keeps polling the `ready` checks.

App output goes to `.axx/logs/apps.log`. When an app fails to start, the error shows the last lines of its output and a stable code from [`AXX-E0400` to `AXX-E0415`](/references/error-codes/#app-lifecycle).

## Keep apps running between runs

```sh
axx up            # start every enabled app (or: axx up api) and wait until ready
axx run           # reuses the running apps: no start, no stop
axx run --tags @smoke
axx down          # stop the apps and run their cleanup
```

This is the fastest local loop, and the one agents should use.

## When a run is stopped by force

A run that is killed before it can stop its apps (stopped by force from an editor, say, or crashed) leaves them in `.axx/run/state.json`. The next run cleans up first, as `axx down` does: it stops what is left, runs the cleanups, says so, and starts from a clean slate. What belongs to a run still going, or to `axx up`, is left alone: a run then starts no app and says what is left (`AXX-E0415`).

## When a cleanup fails

A cleanup can fail: `docker compose down` without access to Docker, say. The run reports it (`AXX-E0411`), and `.axx/run/state.json` keeps the cleanup, so what it should have removed is not forgotten. Until it succeeds:

- runs and `axx up` try it again first, and start no app while it fails, saying what is left (`AXX-E0415`): containers, volumes and requests recorded by a mock would be where the next run starts from;
- `axx down` runs the cleanup again, and says `cleaned up after an earlier run` once it succeeds;
- `axx doctor` lists what is left, and so does the MCP `env` tool's `status`, with each app `running`, `left over` from a run that was killed, or `not cleaned up`.

## Run an app yourself

To run the service from your IDE (with breakpoints, hot reload, a profiler), tell Axx not to start it. Axx still starts everything else and waits for your app's readiness checks:

```sh
axx run --attach api
```

`--no-start` skips starting, stopping and cleaning up every app; use it when the whole system is already running somewhere.

## Debug an app

Give the app a debug command and a debugger to wait for:

```yaml title="axx.yaml"
apps:
  api:
    command: ./gradlew bootRun
    debug:
      command: ./gradlew bootRun -PappJvmArgs=-agentlib:jdwp=transport=dt_socket,server=n,address=localhost:5005,suspend=n
      debugger:
        type: java            # java, go, nodejs or python
        port: 5005
        mode: ide-listens     # the IDE listens, the app connects (default for java)
      onUnavailable: retry    # retry (default), fail, or fallback to the normal command
      retry: {attempts: 10, delay: 3s}
```

```sh
axx run --debug          # every app with a debug block
axx run --debug=api      # only api
axx up --debug=api
```

With `mode: ide-listens`, start a listening debugger in your IDE first (in IntelliJ IDEA: a *Remote JVM Debug* configuration in *Listen to remote JVM* mode on port 5005), then run Axx. With `mode: app-listens` (delve, `--inspect`, debugpy), the app opens the port and you attach; the [parcels example](https://github.com/nimbusxr/axx/tree/main/examples/parcels) runs its Go service under Delve this way. If no debugger is listening, `onUnavailable` decides whether to wait, fail with [`AXX-E0409`](/references/error-codes/#axx-e0409), or run without debugging.

## Start only what a run needs

In a repository with several services, start only the apps the selected scenarios need:

```yaml title="axx.yaml"
active:
  enabled: true
  onNoTags: fallback       # no selected scenario has a tag: start every enabled app (or: error)

apps:
  orders:
    command: ./gradlew :orders:bootRun
    active: {tags: ["@orders"]}
  billing:
    command: ./gradlew :billing:bootRun
    active: {tags: ["@billing", "@payments"]}
  wiremock:
    command: docker compose up wiremock   # no active.tags: always starts
```

An app with `active.tags` starts when any selected scenario carries one of its tags, and the apps in its `dependsOn` start with it. Apps without `active.tags` always start. `axx run --tags @billing` starts `billing` and `wiremock`, not `orders`.
