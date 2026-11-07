---
title: Run beside your own stack
description: Keep your own Docker stack running while Axx starts a separate test stack from the same compose file, on ports Docker picks.
---

Many developers keep their Docker stack running all day. If Axx starts its test services from the same compose file, two problems come up:

1. **Both are the same stack.** Docker Compose names a stack after its folder. When Axx runs `docker compose up`, it takes over your running containers. When Axx cleans up with `docker compose down -v`, it deletes them, and your local data with them.
2. **Both want the same ports.** Your stack already uses ports like 5433 and 8800, so Axx's stack can't start on them. Worse, Axx may find your stack on those ports and run the tests against your data.

Axx doesn't run Docker itself. It runs the command you give each service in `axx.yaml`. This guide changes that command so the test stack gets its own name and its own ports. The [parcel-support example](https://github.com/nimbusxr/axx/tree/main/examples/parcel-support) is set up this way.

## Step 1: Give the test stack its own name

Pass a project name with `-p` in both the command and the cleanup:

```yaml title="axx.yaml"
services:
  support:
    dir: ../infra
    command: docker compose -p parcel-support-tests up --build
    cleanup: docker compose -p parcel-support-tests down -v --remove-orphans
```

Your stack and the test stack are now separate. Neither one starts, stops or deletes the other's containers.

## Step 2: Let Docker pick the test stack's ports

In the compose file, publish each port through a variable. The number after `:-` is the port your own stack uses:

```yaml title="compose.yaml"
services:
  postgres:
    ports:
      - '${SUPPORT_DB_PORT:-5433}:5432'
  support:
    ports:
      - '${SUPPORT_PORT:-8800}:8800'
```

When you run `docker compose up` yourself, these variables aren't set, so your stack keeps ports 5433 and 8800.

For the test stack, set the variables to `0` in the service's `env`. Port `0` tells Docker to pick any free port:

```yaml title="axx.yaml"
services:
  support:
    env:
      SUPPORT_PORT: "0"
      SUPPORT_DB_PORT: "0"
```

## Step 3: Tell Axx which ports Docker picked

Axx gives each service's command a file to write values to. The file's path is in the environment variable `AXX_PROPERTIES`. Each line in it is `name=value`, for example `support.port=55123`. Axx reads the file and makes each value available as `${sys:name}`, both in your scenarios and in the service's readiness check.

This script starts the stack, asks Docker which ports it picked, and writes them to that file:

```sh title="infra/test-stack.sh"
#!/bin/sh
docker compose -p parcel-support-tests up --build &
up=$!

# write_port <compose service> <container port> <name for Axx>
write_port() {
  # `docker compose port` prints the address Docker published, like 0.0.0.0:55123.
  until address=$(docker compose -p parcel-support-tests port "$1" "$2" 2> /dev/null) && [ -n "$address" ]; do
    kill -0 "$up" 2> /dev/null || exit 1   # the stack stopped before it got a port
    sleep 1
  done
  echo "$3=${address##*:}" >> "$AXX_PROPERTIES"   # keep only the port, after the last colon
}

write_port support 8800 support.port
write_port postgres 5432 support-db.port
wait "$up"   # keep running while the stack runs, so Axx can stop it at the end
```

Run the script as the service's command. The readiness check uses the port the script wrote:

```yaml title="axx.yaml"
services:
  support:
    dir: ../infra
    command: ./test-stack.sh
    env:
      SUPPORT_PORT: "0"
      SUPPORT_DB_PORT: "0"
    cleanup: docker compose -p parcel-support-tests down -v --remove-orphans
    ready:
      http:
        url: http://localhost:${sys:support.port}/health
```

So do the scenarios:

```gherkin title="features/messages/messages.feature"
Background:
  Given the support service with the following properties:
    | url | http://localhost:${sys:support.port} |
```

## Which value Axx uses

A `${sys:name}` can get its value from three places. Axx uses the first one on this list that has it:

1. `-D name=value` on the command line.
2. The `properties` in `axx.yaml`, including those of any profile you run with.
3. A value a service wrote to its `AXX_PROPERTIES` file.

In short, **a value you set yourself always wins.** Values that services write only fill in the names you didn't set.

When a service writes a value that you also set, Axx uses yours and prints a line saying so:

```
axx: using support.port=8800 from axx.yaml, not 55123, which service support wrote: a value you set wins (remove it from axx.yaml's properties, or the profile's, to use service support's)
```

## Don't set these ports in axx.yaml

Because a value you set wins, don't put `support.port` or `support-db.port` in the `properties` of `axx.yaml`. If you do, Axx uses that port and ignores the one Docker picked. The readiness check and the tests then reach whatever answers on that port, which is usually your own stack.

Leave them out. Your own stack's ports are already in the compose file (`${SUPPORT_PORT:-8800}`).

## Run the tests against your own stack

Sometimes you do want the tests to run against the stack you started yourself. Add a profile that sets your stack's ports and turns off the test stack:

```yaml title="axx.yaml"
profiles:
  own-stack:
    properties:
      support.port: "8800"
      support-db.port: "5433"
    services:
      support:
        enabled: false   # your stack is already running, so Axx starts nothing
```

Then run:

```sh
axx run --profile own-stack
```

For a one-off run without a profile, `-D` does the same:

```sh
axx run --no-start -D support.port=8800 -D support-db.port=5433
```

## Services that need these ports

A service that Axx runs directly on your machine, like an API started with `go run`, can use the ports too. Use `${sys:…}` in its `command`, `env` or `cleanup`, and list the service that writes the ports in its `dependsOn`:

```yaml title="axx.yaml"
services:
  api:
    command: go run ./cmd/api
    dependsOn: [support]
    env:
      DB_URL: postgres://localhost:${sys:support-db.port}/support
```

Axx fills in these values when it starts the service. Because of `dependsOn`, `api` starts only after `support` is ready, and by then the script has written the ports.

If no service in `axx.yaml` has `dependsOn`, Axx starts the services one at a time, in the order `axx.yaml` lists them. Each service can then use the values written by the services listed above it.

If a service starts before a value it uses has been written, Axx stops the run with [`AXX-E0418`](/references/error-codes/#axx-e0418) and tells you which service to add to its `dependsOn`.

Containers in the same stack don't need any of this. They reach each other by service name and container port, like `postgres:5432`, and those never change.

## Kafka, and other containers that hand out their own address

Most clients connect to the address you give them and stay there. A Kafka client doesn't. It first connects to the broker, and the broker replies with the address the client should use from then on, called the advertised listener. In the test stack, that address must contain the port Docker picked.

Docker picks the port before the container starts, but it doesn't tell the container which port it picked. So the script hands the port to the container, and Kafka's container waits for it before starting Kafka.

In the compose file, replace Kafka's command with one that waits for the port:

```yaml title="compose.yaml"
services:
  kafka:
    image: apache/kafka-native:3.9.1
    ports:
      - '${KAFKA_PORT:-9092}:9092'
    command:
      - /bin/sh
      - -c
      - |
        port=$$KAFKA_PORT
        while [ "$$port" = 0 ]; do sleep 0.2; port=$$(cat /tmp/host-port 2> /dev/null || echo 0); done
        export KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://localhost:$$port,INTERNAL://kafka:9094
        exec /etc/kafka/docker/run
    environment:
      KAFKA_PORT: ${KAFKA_PORT:-9092}
      KAFKA_LISTENERS: PLAINTEXT://kafka:9092,CONTROLLER://kafka:9093,INTERNAL://kafka:9094
      KAFKA_INTER_BROKER_LISTENER_NAME: INTERNAL
      # ...the rest of your broker's settings
```

- In your own stack, `KAFKA_PORT` is 9092, so Kafka starts right away and advertises `localhost:9092`.
- In the test stack, `KAFKA_PORT` is `0`. The container waits until the file `/tmp/host-port` holds the real port, then starts Kafka advertising that port.

In the script, put these lines before the other `write_port` lines. Containers that depend on Kafka won't start until Kafka does, so their ports can't be read until then:

```sh title="infra/test-stack.sh"
write_port kafka 9092 kafka.port

# Hand the same port to Kafka's container, so Kafka can start.
port=$(docker compose -p parcel-support-tests port kafka 9092)
docker compose -p parcel-support-tests exec -T kafka sh -c "echo ${port##*:} > /tmp/host-port"
```

Other containers in the stack keep reaching Kafka at `kafka:9094`.

## Details

- **Each service gets its own file.** It lives in `.axx/properties`. Axx empties it when the service starts and deletes it when the service stops.
- **The file's format.** Blank lines are fine. Any other line that isn't `name=value` stops the run with [`AXX-E0416`](/references/error-codes/#axx-e0416).
- **Which value wins.** A value you set yourself, with `-D` or in `axx.yaml`, wins over one a service wrote. See [Which value Axx uses](#which-value-axx-uses).
- **Readiness checks** (`ready.http`, `ready.tcp` and `ready.exec`) read the values each time Axx tries them. A check that uses a value that hasn't been written yet keeps waiting.
- **Two services can't write the same name.** If they do, Axx stops the run with [`AXX-E0417`](/references/error-codes/#axx-e0417) instead of picking one. Name each value after what it is, like `support.port` and `support-db.port`.
- **`axx up`** keeps the values until `axx down`, so every `axx run` in between reads them.
- **The [axx host](/guides/run-from-a-sandbox/).** When the host starts the services, they write their values in a folder only the host can write to, and the host passes the values to the run.
- **A hook before `axx run`** gets its own `AXX_PROPERTIES` file too. See [Run your commands around axx's](/guides/run-hooks/).
- **Escaping in axx.yaml.** Axx fills in `${…}` in `axx.yaml` itself. If you write a shell command directly in `axx.yaml` and need the shell's own `${…}`, write it as `$${…}`. A separate script file, like the one above, needs no escaping.

## What can't use these values

Some settings are read when Axx loads `axx.yaml`, before any service has started, so they can't use values a service writes: a service's `dir`, and the settings of packs under `packs:`.

Ports that point the other way, from a container back to your machine (`host.docker.internal`), aren't affected by any of this.
