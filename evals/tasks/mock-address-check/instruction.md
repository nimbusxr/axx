Before the parcels service registers a parcel, it asks our address service whether we
deliver to the recipient's postcode (see `docs/registration.md`). We have no acceptance
tests for that integration yet. Please add them to this repository (see `README.md` for the
service and how the tests run).

Acceptance criteria:

1. Registering a parcel checks the recipient's postcode with the address service, exactly
   once, and the service authenticates with our API key (the `X-Api-Key` header; the key is
   `evals-address-key` in this environment).
2. A registered parcel carries the delivery zone the address service returned for its
   postcode.
3. When the address service says a postcode is not deliverable, the registration is refused
   with **422** and the parcel is not stored. (In this environment the address service
   answers "not deliverable" for postcodes that start with `999`.)

The address service is a WireMock mock (`http://address-service:8080`, stubs in
`infra/address-service/mappings/`); leave the stubs as they are. The tests must pass against
the service as it is today and fail if any of these behaviors breaks. Put them in `features/`
(seed data, if you want any, in `seeds/`); don't change anything else in the repository.
