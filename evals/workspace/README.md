# Parcels: acceptance tests

Black-box acceptance tests for **parcels**, our parcel-registration service. Shops register
parcels through the REST API or upload manifests in bulk; depot scanners feed the tracking
view; every registered parcel gets a shipping label and a `ParcelRegistered` event.

The tests are Gherkin features in `features/`, run with [axx](https://axx.nimbusxr.us)
(`axx.yaml` is the configuration).

## The service

| What | Where |
| --- | --- |
| Binary | `parcels` (on the `PATH`); `axx.yaml` starts it as the app `parcels` |
| API | `http://localhost:8080`, health at `/health` |
| OpenAPI 3.1 | `http://localhost:8080/openapi.json` (also `openapi.yaml` in this repository) |
| Business rules | `docs/` (registration, manifest import, tracking, labels, events) |

## Infrastructure

The environment runs these next to the service:

| Service | Address | Credentials |
| --- | --- | --- |
| PostgreSQL | `postgres:5432`, database `parcels`, schema `parcels` | `parcels` / `parcels` |
| MongoDB | `mongo:27017`, database `parcels` (authenticate against `admin`) | `parcels` / `parcels` |
| Address service (WireMock) | `http://address-service:8080`; stubs in `infra/address-service/mappings/` | none |
| Kafka and Schema Registry | `kafka:9092`, `http://schema-registry:8081` (only where events are enabled) | none |

`axx.yaml` defines `${sys:...}` properties for these addresses.

The service authenticates to the address service with the API key `evals-address-key`, and
signs shipping labels with the key `evals-label-secret` (test environment values).

## Test data

Data stays in the databases, the address service's request journal and Kafka until it is
wiped. `parcels reset` wipes all of it; axx runs it after each `axx run` (the app's
`cleanup`), but not between runs while the app is kept up with `axx up`.
