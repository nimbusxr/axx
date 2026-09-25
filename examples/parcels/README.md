# Parcels: an axx example

A complete acceptance suite for a realistic service. **Parcels** is a parcel-delivery
service written in Go: shops ask it for price quotes, register parcels through its API or
upload them in bulk as manifests, and follow them through the depots. It stores parcels in
PostgreSQL, keeps a tracking read model in MongoDB, checks every address with a downstream
address service, and talks to the depots over Kafka.

axx tests it black-box: it starts the service and its infrastructure with Docker Compose,
waits until the service is healthy, runs the features against it and cleans up afterwards.
The service has no dependency on axx or on any test framework.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `quotes` | prices by service level, weight, destination and delivery zone; no quote for undeliverable postcodes; undocumented fields from the address service do not break quoting | requests built from the OpenAPI examples, a Scenario Outline, a mocked dependency, a dependency's contract relaxed for one scenario |
| `register-parcels` | register, look up, change, cancel and list parcels; unique references; the 30 kg limit; signed labels | OpenAPI validation and its levels, ordered requests, null and undefined properties, regular expressions, SQL selections |
| `address-check` | every registration asks the address service with the API key; undeliverable and unavailable addresses | WireMock verification, the address service's OpenAPI contract checked on every call, entries in two logs (the service's and the mock's) |
| `manifest-import` | manifest lines become parcels or are rejected with the reason | YAML, flat XML and CSV seeds, polling selections, JSON and JSONB column checks, entries in the console log |
| `dispatch` | a parcel the depot is dispatching cannot be changed until the depot lets go of it | row locks |
| `database-failures` | brief database failures are retried; lasting ones ask the shop to try again later | fault-injecting triggers, `@isolated` scenarios, counting log entries (one per retry) |
| `tracking` | the tracking summary shows the latest scan, however late or often scans arrive | MongoDB seeds and polling queries, publishing Avro events |
| `registration-events` | every registered parcel is announced as a ParcelRegistered event, and a refused one is not | consuming Avro events through the Schema Registry, a log entry that proves an event was not published |

`axx.yaml` also shows test-data lint rules (`axx lint`), fixture factories (`axx fixtures`)
and debugging the service from your IDE (`axx run --debug`).

## Layout

```
parcels/
  app/          the system under test: a Go module with its Dockerfile
  infra/        compose.yaml plus the files the containers mount
    postgres/     the database schema (the seeds must match it)
    openapi/      the address service's OpenAPI contract
    wiremock/     the address service mock: mappings and response bodies
  acceptance/   the axx project
    axx.yaml      run settings, the app definition, lint rules, fixture settings
    features/     the features
    seeds/        database seeds (YAML and JSON, plus generated XML and CSV datasets)
    kafka/        depot scan events (generated from a factory)
    schemas/      Avro schemas
```

## Run it

You need Docker with Compose v2, and axx:

```sh
go install github.com/nimbusxr/axx/cmd/axx@latest
```

Then, from `acceptance/`:

```sh
cd acceptance
axx fixtures generate   # once: writes the generated (gitignored) fixtures
axx run
```

`axx run` reads `axx.yaml` and:

1. starts the `parcels` app: `docker compose up --build` in `../infra`, which builds the
   service and starts it with all of its infrastructure;
2. waits until `http://localhost:8400/health` answers (the first build takes a few
   minutes);
3. runs every feature in `features/` in parallel; the `@isolated` scenarios, which make
   the parcels table fail on purpose, run alone afterwards;
4. stops compose and runs `docker compose down -v --remove-orphans`.

Other useful commands, all from `acceptance/`:

```sh
axx validate                            # check every step, without running anything
axx run features/quotes.feature         # run one feature
axx lint                                # test-data isolation rules
axx up                                  # keep the service running between runs (axx down stops it)
```

The scenarios register parcels with fixed references and seed fixed rows, and the data
stays in the database. Against a service kept running with `axx up`, the stateless quote
scenarios can run again and again; for the others, start fresh with `axx down` and
`axx up`, or use `axx run`, which always starts from empty databases.

## Infrastructure

`infra/compose.yaml` defines everything, with healthchecks, so you can also start the
system by hand (`docker compose up --build --wait` in `infra/`) and point axx, curl or a
browser at it. The host ports are the ones the features use:

| Service | Image | Host port | Purpose |
| --- | --- | --- | --- |
| `app` | built from `../app` | 8400 | the parcels API under `/api`, OpenAPI at `/openapi.json`, health at `/health` |
| `address-service` | built from `extensions/wiremock-openapi` | 8081 | WireMock with axx's OpenAPI validation extension: the mocked address service |
| `postgres` | `postgres:16` | 5432 | parcels and manifest lines |
| `mongo` | `mongo:7` | 27017 | depot scans and the tracking read model |
| `kafka` | `apache/kafka-native:3.9.1` | 9092 | single-node KRaft broker |
| `schema-registry` | `confluentinc/cp-schema-registry:7.9.2-1-ubi8` | 9081 | Avro schemas for the events |

The address service mock builds the WireMock extension from this repository
(`extensions/wiremock-openapi`). The same image is published as
`ghcr.io/nimbusxr/axx-wiremock`; replace `build:` with `image:` to use it instead. It checks every
call against `openapi/address-service.yaml` (`OPENAPI_SPEC_SOURCE`) in `report` mode: the service
gets the stubs' answers unchanged, and a call that breaks the contract fails the scenario that
checks it, or the run when no scenario does.

The service sends every log line to `udp://0.0.0.0:5140`, where axx listens during a run
(the `parcels` log in the features); axx opens that listener before it starts compose. The
`console` log is the output axx captures from compose, in `.axx/logs/apps.log`.

Kafka advertises `localhost:9092` to clients on the host. Set `LOCAL_HOST` before starting
compose to advertise another host name, and pass the same name to axx
(`axx run -D local.host=<name>`) so the Kafka and mock steps use it too.

### Service configuration

The service reads its settings from environment variables. The defaults reach the
published ports on `localhost`, which is what you want when running it on the host;
`compose.yaml` points them at the compose services instead.

| Variable | Default | In compose |
| --- | --- | --- |
| `PARCELS_ADDR` | `:8400` | `:8400` |
| `PARCELS_DB_URL` | `postgres://parcels:parcels@localhost:5432/parcels` | `...@postgres:5432/parcels` |
| `PARCELS_MONGO_URI` | `mongodb://parcels:parcels@localhost:27017/` | `...@mongo:27017/` |
| `PARCELS_ADDRESS_URL` | `http://localhost:8081` | `http://address-service:8080` |
| `PARCELS_KAFKA_BROKERS` | `localhost:9092` | `kafka:9094` |
| `PARCELS_SCHEMA_REGISTRY_URL` | `http://localhost:9081` | `http://schema-registry:8081` |
| `PARCELS_LOG_UDP` | (none) | `host.docker.internal:5140`: every log line also goes to axx's log listener |

## Debug the service from your IDE

```sh
cd acceptance
axx run --debug
```

In debug mode axx runs the `debug.command` from `axx.yaml` instead of the normal command:
compose starts only the infrastructure, and the service runs on the host under
[Delve](https://github.com/go-delve/delve), listening for a debugger on port 2345. Attach
your IDE to `localhost:2345` (in IntelliJ IDEA or GoLand: a **Go Remote** configuration;
`axx ide intellij` writes one), set breakpoints in `app/`, and they are hit while the
scenarios run. You need `dlv` on your `PATH`
(`go install github.com/go-delve/delve/cmd/dlv@latest`).
