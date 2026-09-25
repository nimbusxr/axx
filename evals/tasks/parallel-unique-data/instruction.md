Our CI runs the acceptance suite with many parallel workers, in random order, against one
shared database. We need acceptance tests for how a shop lists its parcels
(`GET /api/parcels?sender=<shop>`, see `docs/registration.md`), written so that no scenario
can ever be disturbed by another one. Please add them to this repository (see `README.md` for
the service and how the tests run).

Acceptance criteria:

1. A shop's list contains exactly its own parcels, oldest first, and never another shop's.
2. A shop without parcels gets an empty list.
3. A cancelled parcel disappears from its shop's list.

Set up each scenario's parcels with seed files in `seeds/` rather than through the API. Also
add a test-data isolation rule to `axx.yaml` so that `axx lint` fails as soon as two seed files
use the same parcel reference.

The tests must pass against the service as it is today, however many workers run them and in
whatever order, and fail if any of these behaviors breaks. Put them in `features/` and
`seeds/`; in `axx.yaml`, change only the `lint` section.
