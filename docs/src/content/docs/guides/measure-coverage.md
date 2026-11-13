---
title: Measure coverage
description: Find out how much of your services' code the scenarios run, in JVM, Go, Python and Node.js services or any language that writes lcov, and fail the run when it drops under a minimum.
---

A run with coverage measures how much of the system under test the scenarios run: the code of the services Axx starts, and the JavaScript of the web apps' pages. When the run ends, Axx writes one report of all of it and prints the totals:

```text
coverage: lines 71.5%, branches 62.1%, functions 80%, in .axx/coverage/lcov.info
  orders (jvm): lines 76.2%, branches 64.3%, functions 82.4%
  shipments (go): lines 58.9%, functions 70%
  web (web): lines 90.32%, branches 89.47%, functions 100%
```

Coverage is measured for the whole run, from the moment the services start until they stop. It isn't measured per scenario.

## Set it up

1. Add the pack of each runtime your services run on. Each one brings `coverage-core`, which writes the report.

   ```sh
   axx pack add coverage-jvm      # Kotlin, Java, Scala or Groovy, with JaCoCo
   axx pack add coverage-go       # Go, with go build -cover
   axx pack add coverage-python   # Python, with coverage.py
   axx pack add coverage-node     # Node.js, with V8's own coverage
   ```

   To count the web apps' JavaScript too, add `web-coverage` (see [Measure JavaScript coverage](/guides/web-coverage/)).

2. Give each service you want measured a `coverage` section in `axx.yaml`. Its `runtime` names the pack that measures it. The sections below show each runtime's.

   ```yaml title="axx.yaml"
   services:
     orders:
       dir: ../orders
       command: java -jar build/libs/orders.jar
       coverage:
         runtime: jvm
   ```

3. Run with coverage:

   ```sh
   axx run --coverage
   ```

   To measure every run, for example in CI, set `run.coverage: true` in `axx.yaml` instead.

A service without a `coverage` section runs as usual and isn't measured.

## How a service is measured

Axx gives a measured service what its runtime needs, as environment variables, when the run starts it. Most runtimes write what ran when the process exits. So when the run stops the service, the service must exit on its stop signal, and not be killed once its grace runs out (`stop.grace`). If a service wrote nothing, the run says why, from how the service ended: for example, that it was killed, or that it exited before the run stopped it.

:::caution[On Windows]
Windows has no stop signal for a process. Axx ends a service that runs on a Windows machine at once, so the service can't write its coverage. To measure it on Windows, run it in a container: Docker stops a container's processes with a stop signal, inside Linux. A service that writes its own report (`report`) is measured on Windows if it writes the report before axx stops it.
:::

The `coverage` section can change how the service starts when the run measures it:

| Field | What it does |
| --- | --- |
| `runtime` | What measures the service: `jvm`, `go`, `python`, `node`, or `report` for a service that writes its own report. |
| `command` | The command to start the service with when the run measures it. It replaces `command`, as `debug.command` does in debug mode. |
| `env` | More environment variables for the service when the run measures it. |
| `mount` | Where a container mounts the service's coverage folder. See [Services in containers](#services-in-containers). |

A service started in debug mode (`axx run --debug`) isn't measured.

### JVM services

`coverage-jvm` gives every JVM the service starts JaCoCo's agent, through `JAVA_TOOL_OPTIONS`. The agent writes what ran when the JVM exits. Axx then reads it against the service's compiled classes, which must be the ones the service ran.

```yaml title="axx.yaml"
services:
  orders:
    dir: ../orders
    command: java -jar build/libs/orders.jar
    coverage:
      runtime: jvm
packs:
  coverage-jvm:
    parts:
      orders:
        classes: [../orders/build/classes/kotlin/main]
        sources: [../orders/src/main/kotlin]
```

By default, the classes are those of a Gradle or Maven build in the service's `dir` (`build/classes/*/main` or `target/classes`), and the sources are `src/main/kotlin` and `src/main/java` there. Only the files in the sources count, so a library's classes never do.

`classes` can also name the service's jar. For a Spring Boot jar or a web archive (`.war`), Axx reads only the application's own classes (`BOOT-INF/classes` or `WEB-INF/classes`), not the libraries packed beside them.

If the service's image builds the jar itself, with a multi-stage Dockerfile, the classes it ran are only in the image. Copy the jar out of the image, and name it in `classes`:

```sh
docker create --name orders-classes shop-orders
docker cp orders-classes:/app/orders.jar build/orders.jar
docker rm orders-classes
```

If the run can't find the classes or the sources, it says so for that service, and what to set.

- Start the service's own JVM in its command, with `java -jar`, not through Gradle or Maven: their own JVMs would get the agent too.
- `JAVA_TOOL_OPTIONS` replaces the service's own. Put the service's JVM options in its command.
- Reading the coverage needs Java on the machine that runs Axx. Axx downloads JaCoCo from Maven Central the first time.

### Go services

A Go service is measured when it's built for coverage, with `go build -cover`. Its `coverage.command` builds it so and runs the build. Axx sets `GOCOVERDIR`, the folder Go writes to as the program exits.

```yaml title="axx.yaml"
services:
  shipments:
    dir: ../shipments
    command: go run ./cmd/shipments
    shell: true   # its commands run through the shell, for the &&
    coverage:
      runtime: go
      command: go build -cover -coverpkg=./... -o bin/shipments-cover ./cmd/shipments && bin/shipments-cover
```

- `-coverpkg=./...` counts every package of the module. Without it, Go counts only the main package.
- Go writes its coverage only when `main` returns or the program calls `os.Exit`. On SIGTERM, the service must stop and return from `main`, for example with `signal.NotifyContext`.
- Axx names the files of the module of the service's `dir`. If the module is elsewhere, name its folder in `packs.coverage-go.parts.<service>.modules`.
- Go's coverage counts lines and functions. It has no branches.

### Python services

A Python service runs under coverage.py, installed in the service's own environment. Its `coverage.command` starts it with `python -m coverage run`. Axx gives coverage.py its settings through `COVERAGE_RCFILE`, so that the data goes to the service's coverage folder, branches are counted, and the data is written on SIGTERM too.

```yaml title="axx.yaml"
services:
  claims:
    dir: ../claims
    command: python -m claims
    coverage:
      runtime: python
      command: python -m coverage run -m claims
```

- Every Python process of the service is measured: the processes it starts, such as uvicorn's workers, and the ones it forks, such as gunicorn's. coverage.py must be installed where they run, as it is when the service's image installs it with pip.
- coverage.py doesn't measure code installed as a package, in site-packages, as `pip install .` installs it. To measure it, name the service's packages, and map where they're installed to the project's files:

  ```yaml title="axx.yaml"
  packs:
    coverage-python:
      parts:
        claims:
          packages: [claims]
    coverage-core:
      parts:
        claims:
          paths: {/usr/local/lib/python3.13/site-packages/: ../claims/}
  ```

- In a run with coverage, Axx's settings replace the project's own coverage.py settings.
- Axx reads the data with a coverage.py of its own, which needs Python 3.11 or later on the machine that runs Axx. It doesn't run the project's coverage.py plugins.

### Node.js services

`coverage-node` has Node write V8's own coverage of the service (`NODE_V8_COVERAGE`), and gives it a small preload script (`NODE_OPTIONS=--require`). If the service doesn't handle its stop signal itself, the preload makes it exit cleanly on that signal, so that Node writes the coverage.

```yaml title="axx.yaml"
services:
  tracking:
    dir: ../tracking
    command: node dist/server.js
    coverage:
      runtime: node
```

- The service's scripts count through their source maps, so TypeScript counts as TypeScript. `node_modules` and Node's own scripts don't count.
- When the service's image compiles its TypeScript, the compiled scripts are only in the image. Their coverage still counts, in the project's `.ts` files: Node records each script's source map and the length of its lines, and the `.ts` files are in the project. Map the image's paths to the project's in `paths`, as for any service in a container.
- Only the project's files count.
- Axx opens no port and no inspector.

### Any other language

A service in any other language can write its own report, with its own coverage tool. Name the runtime `report`, and have the service write an lcov file (`*.info`) or a Cobertura file (`*.xml`) into the folder `AXX_COVERAGE_DIR` names, by the time it exits. Axx reads it after the service stops.

```yaml title="axx.yaml"
services:
  rating:
    dir: ../rating
    command: ./bin/rating
    coverage:
      runtime: report
      # Runs the service under its coverage tool, which writes
      # $AXX_COVERAGE_DIR/rating.xml (Cobertura) as the service exits.
      command: ./scripts/rating-with-coverage.sh
```

## Services in containers

A service in a container writes its coverage into a folder on the machine that runs Axx, which its compose file mounts. Axx sets `AXX_COVERAGE_DIR` to the service's folder on every run, measured or not, so the compose file can always mount it. `mount` says where the container sees the folder.

This is how the parcels example measures its Go service in compose:

```yaml title="axx.yaml"
services:
  parcels:
    dir: ../infra
    command: docker compose up --build
    coverage:
      runtime: go
      mount: /axx-coverage
      env:
        PARCELS_GO_BUILD_FLAGS: -cover -coverpkg=./...
packs:
  coverage-go:
    parts:
      parcels:
        modules: [../app]
```

```yaml title="compose.yaml"
services:
  app:
    build:
      context: ../app
      args:
        GO_BUILD_FLAGS: ${PARCELS_GO_BUILD_FLAGS:-}
    environment:
      GOCOVERDIR: ${GOCOVERDIR:-}
    volumes:
      - '${AXX_COVERAGE_DIR:-./coverage}:/axx-coverage'
```

The Dockerfile builds with `go build $GO_BUILD_FLAGS`. A Python service runs under coverage.py the same way, through a variable its compose file puts in the container's command:

```yaml title="axx.yaml"
services:
  claims:
    dir: ../infra
    command: docker compose up --build
    coverage:
      runtime: python
      mount: /axx-coverage
      env:
        CLAIMS_PYTHON_RUN: coverage run -m uvicorn
```

```yaml title="compose.yaml"
services:
  claims:
    build: ../claims
    command: python -m ${CLAIMS_PYTHON_RUN:-uvicorn} claims.api:app --host 0.0.0.0 --port 8000 --workers 2
    environment:
      COVERAGE_RCFILE: ${COVERAGE_RCFILE:-}
    volumes:
      - '${AXX_COVERAGE_DIR:-./coverage}:/axx-coverage'
```

The variables the runtime gives the service reach `docker compose`, and the compose file passes them on to the container: `GOCOVERDIR` for Go, `JAVA_TOOL_OPTIONS` for the JVM, `COVERAGE_RCFILE` for Python, and `NODE_V8_COVERAGE` and `NODE_OPTIONS` for Node.js. Their values already name the folder by its `mount`.

- **Users.** The folder is writable by every user, because a container's process often runs as a user of its own, such as distroless's `nonroot`.
- **Paths.** A runtime names files as they were in the container, like `/app/claims/api.py`. Map them to the project in `packs.coverage-core.parts.<service>.paths`:

  ```yaml title="axx.yaml"
  packs:
    coverage-core:
      parts:
        claims:
          paths: {/app/: ../claims/}
  ```

  JVM and Go services don't need this: JaCoCo names files by their package, and Go by their module.
- **Stopping.** When the run stops `docker compose up`, compose stops the containers with SIGTERM. A process that is the container's PID 1 and has no SIGTERM handler of its own doesn't stop on SIGTERM, so it's killed after the compose file's `stop_grace_period`. Go services, Node.js services with the preload, and JVMs stop on it. A Python service whose server doesn't handle SIGTERM still writes its data, but waits out the grace. To avoid that, set `init: true` on its compose service.

## Minimums

A minimum fails the run when the coverage drops under it. Set the whole run's in `packs.coverage-core.minimum`, and a service's in `parts`, in percent of lines, branches or functions:

```yaml title="axx.yaml"
packs:
  coverage-core:
    minimum: {lines: 80, branches: 70}
    parts:
      orders: {minimum: {lines: 85}}
      web: {minimum: {functions: 90}}
```

`web` is the web apps' JavaScript. A minimum on something a runtime doesn't count is a mistake in the configuration, and the run says so: Go counts no branches.

A run checks the minimums only when it runs every scenario its configuration selects. Its `run.tags` in `axx.yaml` still apply. A run that runs fewer still writes its report and prints its totals, and says why it didn't check:

- it names paths, or uses `--tags` or `--name`;
- it's one shard of a run (`--shard`; see [Check CI's shards together](#check-cis-shards-together));
- it uses `--fail-fast` or `--no-start`;
- it was interrupted;
- one of its services wasn't measured, for example one in debug mode.

When every scenario passed but the coverage is under a minimum, the run exits with code 6 and says which minimum:

```text
coverage under the minimum: the run's lines 71.5% (minimum 80%); orders lines 76.2% (minimum 85%)
```

A failed scenario still exits 1. If a measured service wrote no coverage while minimums apply, the run exits 4, an error in the environment, and says why. It never counts the service as 0%.

## Check CI's shards together

A sharded run's shards each write their own report, and none of them checks the minimums. Keep each shard's coverage folder, then check them together:

1. In each shard's job, run `axx run --coverage --shard i/n`, and upload `.axx/coverage` as an artifact.
2. In a job after the shards, download the artifacts into one folder each, and run:

   ```sh
   axx coverage check shard-1/ shard-2/ shard-3/
   ```

`axx coverage check` merges the shards, writes the report into `.axx/coverage` (or `--out`), prints the totals, and exits 6 under a minimum. It refuses a set of shards with one missing or repeated, or shards of different runs, instead of checking only part of the run.

## The report

The run writes its report to `.axx/coverage` (`packs.coverage-core.folder`):

| File | For |
| --- | --- |
| `lcov.info` | editors' coverage views, `genhtml`, and coverage services such as Codecov, Coveralls and SonarQube |
| `coverage-final.json` | Istanbul's format: `npx nyc report --reporter=html` makes an HTML report of it |
| `coverage-summary.json` | the totals, for badges and other tools |
| `run.json` | the run's coverage for `axx coverage check` |

The report names each file relative to the project: the folder of `axx.yaml`. With `--json`, the run's report has a `coverage` object with the totals, each part's, the minimums it's under, and the path of `lcov.info`. See [JSON output](/references/json-output/).

## In your editor

Both editors' axx extensions have **Run with Coverage**. It runs `axx run --coverage` and shows the coverage of every service's files in the editor, whatever their language:

- **VS Code** shows it in the Test Coverage view and in the editor's gutter, with lines, branches and functions.
- **IntelliJ IDEA** shows it in the Coverage tool window, the project view, and the editor's gutter.

See [Set up your editor](/guides/set-up-your-editor/).

## Keep services up between runs

`axx up --coverage` starts the services measured, and keeps them running for the runs after it:

- **JVM and Node.js services** are measured run by run. Each run with coverage starts their counts afresh, and reads them when it ends. Only one run at a time can measure them: a second run with coverage started meanwhile stops, and says that another run measures them. For a JVM service, name the port its agent listens on, in `packs.coverage-jvm.parts.<service>.port`. In a container, publish that port on 127.0.0.1. Axx never picks a port itself.
- **Go and Python services** write their coverage only when they exit. A run with coverage lists them without coverage, and says that `axx down` writes it. When `axx down` stops them, it writes their coverage of every run since `axx up`, and prints the totals.

A run with coverage against services `axx up` started without it stops, and says to start them again with `axx up --coverage`.

## With the axx host

A run from an agent's sandbox can measure coverage through the [axx host](/guides/run-from-a-sandbox/). A run with coverage starts the services differently, with their `coverage` sections and the variables the coverage packs give them. So the host asks you to approve it on its own, the first time.

## Downloads

`coverage-jvm` downloads JaCoCo from Maven Central, and `coverage-python` downloads coverage.py from PyPI, each once and checked against a hash pinned in Axx. On a network that can't reach them, see [Use mirrors](/guides/use-mirrors/).

## Settings

| Setting | |
| --- | --- |
| `packs.coverage-core.folder` | where the run's report goes, in the project (default `.axx/coverage`) |
| `packs.coverage-core.minimum` | the whole run's minimums, in percent: `lines`, `branches`, `functions` |
| `packs.coverage-core.parts.<part>.minimum` | a service's minimums, or the web apps' (`web`) |
| `packs.coverage-core.parts.<part>.paths` | where a service's files are in the project, by where they were as it ran |
| `packs.coverage-jvm.parts.<service>.classes` | the folders and jars of the classes the service runs; of a Spring Boot jar or a web archive, its application's classes |
| `packs.coverage-jvm.parts.<service>.sources` | the folders of its sources |
| `packs.coverage-jvm.parts.<service>.port` | the port its agent listens on, with `axx up --coverage` |
| `packs.coverage-go.parts.<service>.modules` | the folders of the Go modules whose files count |
| `packs.coverage-python.parts.<service>.packages` | the service's packages, measured wherever they're installed, site-packages included |
