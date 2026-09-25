Other teams build on our `ParcelRegistered` events (`docs/events.md`): billing charges by
weight and zone, and the depots plan capacity from them. There are no acceptance tests for
the events yet. Please add them to this repository (see `README.md` for the service, Kafka
and the Schema Registry, and how the tests run).

Acceptance criteria:

1. Registering a parcel through the API publishes a `ParcelRegistered` event on the
   `parcel-events` topic, keyed by the parcel's reference.
2. The event carries the parcel's reference, sender, weight in grams, service level and
   delivery zone, and says the parcel was registered through the API (source `api`).
3. The event is Avro-encoded with the schema registered in the Schema Registry, and it has
   the header `X-Event-Type: ParcelRegistered`.

The tests must pass against the service as it is today, and fail if the event stops being
published or carries wrong data. Put them in `features/` (other files the tests need, such
as seed data, in `seeds/`); don't change anything else in the repository.
