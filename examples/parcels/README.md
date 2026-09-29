# Parcels: an Axx example

A complete acceptance suite for a realistic service. **Parcels** is a parcel-delivery
service written in Go: shops ask it for price quotes, register parcels through its API or
its shop portal, or upload them in bulk as manifests, and follow them through the depots. It stores parcels in
PostgreSQL, keeps a tracking read model in MongoDB, checks every address with a downstream
address service, books express collections and pickups with a courier, talks to the depots over Kafka, sends labels to the depots'
printers and hears their reports over AMQP (RabbitMQ), hears the depots' handheld scanners over MQTT, confirms
deliveries and tells other services of every tracking update over NATS, takes the courier's signed callbacks and
tells shops of deliveries in signed webhooks, serves the shops' systems with tokens, caches in Valkey, emails shops, writes each imported manifest's documents to
an export folder, streams depot scans as they happen (over a websocket and as server-sent events), and has an admin
command for its operations desk.

Axx tests it black-box: it starts the service and its infrastructure with Docker Compose,
waits until the service is healthy, runs the features against it and cleans up afterwards.
The service has no dependency on Axx or on any test framework.

## What the features check

| Feature | Acceptance criteria | What Axx uses |
| --- | --- | --- |
| `quotes` | prices by service level, weight, destination and delivery zone; no quote for undeliverable postcodes; undocumented fields from the address service do not break quoting; beyond the EU, the partner carrier's rating service prices the parcel, and a country it does not deliver to gets no quote | requests built from the OpenAPI examples, a Scenario Outline, a mocked dependency, a dependency's contract relaxed for one scenario, a gRPC dependency mocked by WireMock |
| `register-parcels` | register, look up, change, cancel and list parcels; a shop's order system registers what it exports; the courier collects an express parcel without the recipient's name, and only express parcels; cancelling one calls off its collection; unique references; the 30 kg limit; signed labels | OpenAPI validation and its levels, ordered requests, payloads from a fixture factory's files, the JSON body a mock received (a property that must be absent included), a mocked request named by its path and checked by its query parameters, requests a mock did not receive (by their body and their query), null and undefined properties, regular expressions, SQL selections |
| `address-check` | every registration asks the address service with the API key; undeliverable and unavailable addresses | WireMock verification, the address service's OpenAPI contract checked on every call, entries in two logs (the service's and the mock's) |
| `manifest-import` | manifest lines become parcels or are rejected with the reason | YAML, flat XML and CSV seeds, polling selections, JSON and JSONB column checks, entries in the console log |
| `manifest-documents` | an imported manifest's report, summary and handover note land in the export folder | a folder the service writes to, the rows of a CSV file and of a workbook, a file compared byte for byte, JSON properties, the text of a PDF |
| `dispatch` | a parcel the depot is dispatching cannot be changed until the depot lets go of it | row locks |
| `database-failures` | brief database failures are retried; lasting ones ask the shop to try again later | fault-injecting triggers, `@isolated` scenarios, counting log entries (one per retry) |
| `tracking` | the tracking summary shows the latest scan, however late or often scans arrive | MongoDB seeds and polling queries, publishing Avro events |
| `registration-events` | every registered parcel is announced as a ParcelRegistered event, and a refused one is not | consuming Avro events through the Schema Registry, a log entry that proves an event was not published |
| `shop-portal` | the portal's quotes and registration form, a shop's parcels, phones, pickup times, sessions and going offline | a web app in real browsers (Chromium, and WebKit as an iPhone), forms, an upload, a download, the browser's clock and connection, a session to start with, selectors, screenshots, accessibility audits and Lighthouse scores; the registered parcel checked in the database and on Kafka |
| `portal-parcels` | a parcel's page: its label, its tracking page, cancelling it, reporting a problem, times in the shop's time zone | menus, dialogs, a new browser tab, a downloaded label compared byte for byte, tabs, a locale and a time zone |
| `portal-planning` | planning a parcel's pickup, the courier told of it (and not of a day the shop does not offer), when it cannot be planned, and a shop's settings | drag and drop, the form fields a mock received (and did not), a form sent as a request payload, requests that fail or answer with an error, several options chosen, an upload, the rows the portal wrote |
| `portal-tracking` | the recipient's tracking page: when the parcel arrives, and its depot scans as they happen | requests answered with a file, a recording or late, a slow connection, websocket messages, a Kafka event the page shows |
| `live-tracking` | depot scans reach the tracking page's websocket and the shops' event streams as they happen; a delivered parcel ends its stream; a websocket that names no parcel is closed | a websocket opened, sent to and checked by its messages and its close code, a server-sent event stream checked by its events' type, id and data, Avro events published |
| `label-printing` | every registered parcel's label goes to the printers of its service level; the printers' reports, through their exchange or straight to the service's queue, mark labels printed; reports the service cannot use are set aside with the reason | an AMQP exchange and queues: messages published with a routing key, sent with headers, and checked by their routing key, headers and body, SQL selections |
| `depot-scanners` | a handheld scanner's scan updates the tracking; a scan of an unknown parcel alerts the depot's scanners | MQTT messages published with user properties, a topic filter checked by the topic a message came on, MongoDB selections |
| `tracking-updates` | a courier's confirmation marks a parcel delivered; the other services hear of every scan, and the TRACKING stream keeps the updates | NATS messages published with headers, a subject with a wildcard, a JetStream stream, an Avro event on Kafka |
| `courier-callbacks` | the courier's signed callbacks are recorded, and one signed with another key is refused; a delivery is told to the parcel's shop in a signed webhook | a request signed in a header (HMAC-SHA256), a Standard Webhook's signature checked on the mock that receives it, a mocked dependency's contract, MongoDB selections |
| `shop-api` | a shop's system reads its parcels with a token its platform signs, or one it gets with its client credentials; a token for another shop is refused | a JSON Web Token axx signs, an OAuth 2.0 client credentials token axx gets from the service's token endpoint |
| `cache` | a parcel's tracking view is cached, answers from the cache, and is dropped when a scan changes it; a registration retried with its idempotency key is answered with the parcel it made | Redis keys seeded and checked by their value, their JSON properties and their absence, ordered requests with a header |
| `shop-emails` | a shop gets each registered parcel's label by email, and each delivery when it asked to be told; an email a shop's mail server refuses does not stop the registration | smtp4dev read over IMAP: an email's recipient, sender, subject, text, HTML and attachment; a mail server's refusal stubbed like a WireMock mapping; a log entry |
| `tracking-api` | a shop's system reads a parcel's status over gRPC, and watches its scans until it is delivered; a parcel never registered is not found | a gRPC service read through server reflection, its status codes, a server stream checked by its messages |
| `depot-holds` | a depot reads a parcel, and holds it until the day the recipient chose, but not once it is out for delivery; a parcel the service does not know, and a hold without its day | JSON-RPC calls checked against the service's OpenRPC document, their results and error codes |
| `parcels-graph` | a shop's app reads a parcel through the parcels subgraph, holds it at its depot, and hears it go out for delivery | GraphQL queries, mutations and a subscription (graphql-ws) checked against the schema the service introspects, their data and errors |
| `federated-graph` | through the gateway, a parcel comes with its shop from the shops subgraph; a shop the directory does not know; a field the graph does not have | a federated graph (Hive Gateway) over the service's subgraph and a subgraph WireMock mocks from its schema, the `_entities` call the gateway sent it, a validation rule relaxed for one scenario |
| `parcel-assistant` | the assistant reads the address in a shop's note, and sends a note it cannot read back to the shop; it looks a parcel up before it answers, and never tells the model the recipient's street; for a parcel abroad, it asks the partner carrier; a busy model is asked once more, then the recipient is asked to try again | a model mocked by WireMock (the service speaks OpenAI's API), a tool loop, what the service asked the model: the texts, the tools and the schema, and what it must never send; a mocked MCP server and the tool calls it received |
| `parcels-mcp` | the service's MCP server: assistants see its tools, track parcels, hold them (and not once they are out for delivery), read labels and write delivery updates, over HTTP and over stdio | an MCP server's tools, results that are errors and calls it refuses, the tools' schemas as the contract and a level relaxed, a resource and a prompt, a server run as a command |
| `parcels-agent` | the service's A2A agent: its card and skills, where a parcel is, a hold that asks until which day, a hold that can no longer be placed, a stream of its work, a canceled task, an unknown parcel, and a parcel abroad it asks the partner carrier's agent about | an A2A agent's card, tasks and their states, a reply to a task that asks for input, artifacts, a stream over HTTP+JSON, a mocked A2A agent and the messages it was sent |
| `courier-app` | couriers deliver with the service's Android app: they sign in (and a wrong PIN is refused), see the parcels they deliver today, pull the list down for one given to them since, stay signed in until they sign out, and confirm a delivery, signed for, which the parcel's event stream and its recipient's tracking page then show; every scenario starts signed out | an Android app on an emulator axx starts: a deep link, controls tapped and filled by the names people see, a disabled button, a swipe, the back button, a notification, the app sent to the background and restarted, and a web page and an event stream in the same scenario |
| `courier-app-notifications` | the app asks Android for leave to notify the courier; a courier who does not allow it delivers all the same | a dialog the system shows, dismissed; a delivery the tracking store recorded |
| `operations-desk` | the desk's `parcels admin` command reprints labels, cancels parcels (from its input too) under the API's rules, and lists a shop's parcels | a command run in the service's container, its exit code, output and error output, output compared byte for byte and by its JSON properties, SQL selections |

`axx.yaml` also shows test-data lint rules (`axx lint`), fixture factories (`axx fixtures`)
and debugging the service from your IDE (`axx run --debug`).

## Layout

```
parcels/
  app/          the system under test: a Go module with its Dockerfile
  courier/      the couriers' app: an Android app (Kotlin, Jetpack Compose) the courier-app features use
  infra/        compose.yaml plus the files the containers mount
    postgres/       the database schema (the seeds must match it)
    openapi/        the OpenAPI contracts of the address service, the courier and the shops' webhooks
    wiremock/       the address service mock: mappings and response bodies
    courier/        the courier mock's mappings
    shops/          the mock of the shops' systems, which hear of deliveries: its mapping
    rating/         the partner carrier's rating service mock (gRPC): its descriptor set and mappings
    shop-directory/ the shops subgraph mock: its schema and its entities' mappings
    supergraph/     composes the supergraph the gateway serves, from the subgraphs' SDL
    models/         the model mock, which the parcel assistant asks: its answers' mappings
    partner-carrier/ the partner carrier's MCP server and A2A agent mocks: their description, card and mappings
    exports/        what the service writes to its export folder during a run (not committed)
  acceptance/   the Axx project
    axx.yaml        run settings, the app definition, the packs' settings, lint rules, fixture settings
    axx-packs.yaml  the packs whose steps the features use
    features/       the features
    seeds/          database seeds (YAML and JSON, plus generated XML and CSV datasets)
    reports/        the import report a manifest-documents scenario expects
    invoices/       a customs invoice a shop uploads in the portal
    labels/         the labels a portal scenario downloads and the admin command prints
    logos/          the logo a shop uploads in its settings
    screenshots/    how the portal's pages look, on Linux and on macOS
    network/        answers for the tracking page's requests: a file, and a recording of the service
    kafka/          depot scan events (generated from a factory)
    amqp/           the label printers' reports
    mqtt/           a handheld scanner's scan
    nats/           a courier's delivery confirmation
    redis/          cached tracking views seeded into Valkey
    requests/       registration requests an order system exports (generated from a factory)
    schemas/        Avro schemas
```

## Run it

You need Docker with Compose v2, and Axx:

```sh
go install github.com/nimbusxr/axx/cmd/axx@latest
```

The first command that needs the suite's steps prepares Axx with the packs in
`axx-packs.yaml`, once. Then, from `acceptance/`:

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

The portal's features use web browsers on your machine; the `web-core` pack downloads them the
first time. To see them:

```sh
axx run --profile watch features/shop-portal.feature   # the browsers in windows, slowed down
axx run features/shop-portal.feature --pause-at features/shop-portal.feature:24   # pause before that step, in Playwright's Inspector
```

The couriers' apps' features (tagged `@mobile`) run the apps on devices axx runs. The
Android app's (`@android`) run on an emulator axx starts: they need the Android SDK
(`ANDROID_HOME`), the app built, and the emulator's device, once:

```sh
cd ../courier/android
./gradlew assembleDebug   # the app the features install
./create-emulator.sh      # parcels-pixel, the device axx.yaml names (android.device)
```

The iOS app's (`@ios`) run on simulators axx makes of an iPhone 16 (`ios.device`): they need
a Mac with Xcode and an iOS runtime, and the app built:

```sh
cd ../courier/ios
xcodebuild -project Courier.xcodeproj -target Courier -configuration Debug -sdk iphonesimulator ONLY_ACTIVE_ARCH=NO SYMROOT=build
```

Without them, leave those features out: `axx run --tags "not @mobile"`, or `--tags "not @ios"`
on a machine that runs Android emulators but is not a Mac.

The portal's screenshots are kept for Linux and macOS, each compared on its own platform
(`packs.web-screenshots.platforms` in `axx.yaml`); elsewhere those steps pass without
comparing. When the pages change on purpose, take them again with
`axx run --set packs.web-screenshots.update=true`, on each platform.

Each run also measures how much of the portal's own scripts the scenarios run
(`packs.web-coverage` in `axx.yaml`): the totals print at the end, and
`.axx/web/coverage/lcov.info` has them line by line. The quote page's Lighthouse scenario
audits it as a phone loads it.

The scenarios register parcels with fixed references and seed fixed rows, and the data
stays in the database. Against a service kept running with `axx up`, the stateless quote
scenarios can run again and again; for the others, start fresh with `axx down` and
`axx up`, or leave the service down: `axx run` alone then starts it with empty databases.

## Infrastructure

`infra/compose.yaml` defines everything, with healthchecks, so you can also start the
system by hand (`docker compose up --build --wait` in `infra/`) and point Axx, curl or a
browser at it. The host ports are the ones the features use:

| Service | Image | Host port | Purpose |
| --- | --- | --- | --- |
| `app` | built from `../app` | 8400, 8410 | the parcels API under `/api`, the shop portal under `/portal`, OpenAPI at `/openapi.json`, health at `/health`, JSON-RPC at `/rpc`, GraphQL at `/graphql`, its MCP server at `/mcp`, its A2A agent at `/a2a` (card at `/.well-known/agent-card.json`); the tracking API over gRPC on 8410 |
| `address-service` | built from `extensions/wiremock-openapi` | 8081 | WireMock with Axx's OpenAPI validation extension: the mocked address service |
| `courier` | built from `extensions/wiremock-openapi` | 8082 | the mocked courier, checked against `openapi/courier.yaml`: express collections (JSON) and pickups (a form) |
| `shops` | built from `extensions/wiremock-openapi` | 8083 | the mocked shops' systems, checked against `openapi/shop-webhooks.yaml`: the signed webhooks of deliveries |
| `rating` | built from `extensions/wiremock-openapi` | 8084 | the mocked rating service of the partner carrier, over gRPC (WireMock's gRPC extension) |
| `shop-directory` | built from `extensions/wiremock-openapi` | 8085 | the shops subgraph, which another team owns, mocked field by field from its schema |
| `supergraph` | built from `supergraph/` | (none) | composes the supergraph from the subgraphs' SDL, as a team composes in CI |
| `gateway` | `ghcr.io/graphql-hive/gateway:2.15.1` | 4000 | the federated graph's gateway (Hive Gateway), which serves the supergraph |
| `models` | built from `extensions/wiremock-openapi` | 8086 | the model the parcel assistant asks, mocked in the format of its requests (OpenAI's API) |
| `partner-carrier` | built from `extensions/wiremock-openapi` | 8087 | the partner carrier's MCP server and A2A agent, which the service asks about parcels beyond the EU, mocked from their description and card |
| `postgres` | `postgres:16` | 5432 | parcels, manifest lines, pickups and shops' settings |
| `mongo` | `mongo:7` | 27017 | depot scans and the tracking read model |
| `kafka` | `apache/kafka-native:3.9.1` | 9092 | single-node KRaft broker |
| `schema-registry` | `confluentinc/cp-schema-registry:7.9.2-1-ubi8` | 9081 | Avro schemas for the events |
| `rabbitmq` | `rabbitmq:4.2.9-alpine` | 5672 | label print jobs and the printers' reports (AMQP 0-9-1) |
| `mosquitto` | `eclipse-mosquitto:2.0.22` | 1883 | the depots' handheld scanners (MQTT 5) |
| `nats` | `nats:2.15.0-alpine` | 4222 | couriers' delivery confirmations, and tracking updates in the TRACKING stream (JetStream) |
| `valkey` | `valkey/valkey:9.1.2-alpine` | 6379 | the service's cache: tracking views and idempotency keys |
| `mail` | `rnwood/smtp4dev:3.15.0` | 1025, 1143, 8025 | the service's mail server in the tests: SMTP (1025), IMAP for the features (1143) and its web interface (8025); its recipient expression stubs a shop whose mail server is busy |
| `exports` | `busybox:1.37` | (none) | empties `exports/`, and makes it writable, before the service starts |

The address service mock builds the WireMock extension from this repository
(`extensions/wiremock-openapi`). The same image is published as
`ghcr.io/nimbusxr/axx-wiremock`; replace `build:` with `image:` to use it instead. It checks every
call against `openapi/address-service.yaml` (`OPENAPI_SPEC_SOURCE`) in `report` mode: the service
gets the stubs' answers unchanged, and a call that breaks the contract fails the scenario that
checks it, or the run when no scenario does.

The service also sends every log line over UDP to port 5140 on the host, where Axx listens
during a run (`udp://0.0.0.0:5140`, the `parcels` log in the features); Axx opens that
listener before it starts compose. The `console` log is the output Axx captures from compose,
in `.axx/logs/apps.log`.

Kafka advertises `localhost:9092` to clients on the host. Set `LOCAL_HOST` before starting
compose to advertise another host name, and pass the same name to Axx
(`axx run -D local.host=<name>`) so the Kafka and mock steps use it too.

### Service configuration

The service reads its settings from environment variables. The defaults reach the
published ports on `localhost`, which is what you want when running it on the host;
`compose.yaml` points them at the compose services instead.

| Variable | Default | In compose |
| --- | --- | --- |
| `PARCELS_ADDR` | `:8400` | `:8400` |
| `PARCELS_DB_URL` | `postgres://parcels:parcels@localhost:5432/parcels?sslmode=disable` | `...@postgres:5432/parcels?sslmode=disable` |
| `PARCELS_MONGO_URI` | `mongodb://parcels:parcels@localhost:27017/?authSource=admin` | `...@mongo:27017/?authSource=admin` |
| `PARCELS_ADDRESS_URL` | `http://localhost:8081` | `http://address-service:8080` |
| `PARCELS_COURIER_URL` | `http://localhost:8082` | `http://courier:8080` |
| `PARCELS_KAFKA_BROKERS` | `localhost:9092` | `kafka:9094` |
| `PARCELS_SCHEMA_REGISTRY_URL` | `http://localhost:9081` | `http://schema-registry:8081` |
| `PARCELS_EXPORT_DIR` | `../infra/exports` | `/var/lib/parcels/exports`, mounted from `./exports` |
| `PARCELS_LOG_UDP` | (none) | `host.docker.internal:5140`: every log line also goes to Axx's log listener |

## Debug the service from your IDE

```sh
cd acceptance
axx run --debug
```

In debug mode Axx runs the `debug.command` from `axx.yaml` instead of the normal command:
compose starts only the infrastructure, and the service runs on the host under
[Delve](https://github.com/go-delve/delve), listening for a debugger on port 2345. Attach
your IDE to `localhost:2345` (in IntelliJ IDEA or GoLand: a **Go Remote** configuration,
which `axx ide intellij` writes; in VS Code, `axx ide vscode` writes one), set breakpoints
in `app/`, and they are hit while the scenarios run. You need `dlv` on your `PATH`
(`go install github.com/go-delve/delve/cmd/dlv@latest`).
