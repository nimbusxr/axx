# Parcels API

The contract and documentation of **parcels**, our parcel-registration service: shops register
parcels, look them up, change them before pickup and cancel them.

- `openapi.yaml`: the API contract (OpenAPI 3.1). The running service also serves it at
  `/openapi.json`.
- `docs/registration.md`: the registration rules.

## Running the service

The service is installed in this environment as the `parcels` command. It needs no
configuration here: it connects to its PostgreSQL, MongoDB and address service on its own,
listens on `http://localhost:8080` and answers `GET /health` with 200 once it is ready.
`parcels reset` wipes all of its data (run it only while the service is stopped).

## Acceptance tests

None yet. We want to adopt [axx](https://axx.nimbusxr.us) (installed as `axx`).
