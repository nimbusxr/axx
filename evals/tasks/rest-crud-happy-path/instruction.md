We have no acceptance tests yet for the basic parcel lifecycle in the parcels service.
Please add them to this repository (see `README.md` for the service and how the tests run).

Acceptance criteria:

1. A shop can register a parcel. The service confirms with **201 Created** and returns the
   parcel with status `REGISTERED` and the weight, service level and recipient it was
   registered with.
2. A registered parcel can be looked up by its reference.
3. A shop can change the weight of a registered parcel, and looking the parcel up afterwards
   shows the new weight.
4. A shop can cancel a parcel; after that, the parcel can no longer be found (**404**).

The tests must pass against the service as it is today, and fail if any of these behaviors
breaks. Put them in `features/` (seed data, if you want any, in `seeds/`); don't change
anything else in the repository.
