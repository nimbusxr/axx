Depot scanners write every scan into MongoDB, and the parcels service keeps a tracking view
per parcel from them (`docs/tracking.md`). Customers have complained that tracking sometimes
shows an old status. We have no acceptance tests for the tracking view yet. Please add them to
this repository (see `README.md` for the service and how the tests run).

Acceptance criteria:

1. The tracking view shows the status and location of the most recent scan, even when the
   scans arrive out of order.
2. A scan that a scanner sent twice (same scan id) is counted once.
3. Once a parcel has been scanned as `DELIVERED`, its tracking view says it was delivered.

The tests must pass against the service as it is today, and fail if any of these behaviors
breaks. Put the features in `features/` and the scans you insert in seed files in `seeds/`;
don't change anything else in the repository.
