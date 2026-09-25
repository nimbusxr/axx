This repository holds the API contract (`openapi.yaml`) and the docs of the parcels service,
but no acceptance tests. We want to start writing them with axx (it is installed as `axx`).
Please set the repository up for axx and add its first acceptance test. `README.md` explains
how the service runs in this environment.

Acceptance criteria for the first feature:

1. A shop can register a parcel (**201 Created**) and look it up by its reference afterwards.
2. Registering the same reference a second time is refused with **409 Conflict**.

`axx run` must start the service, run the tests and pass against the service as it is today;
the tests must fail if either behavior breaks. Don't change `README.md`, `openapi.yaml` or
the docs.
