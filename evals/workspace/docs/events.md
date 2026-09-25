# Events

Every registered parcel, whether registered through the API or imported from a manifest,
publishes one `ParcelRegistered` event to the Kafka topic `parcel-events`:

- key: the parcel reference (string);
- value: Avro, Confluent wire format, schema registered in the Schema Registry under the
  subject `parcel-events-value`;
- header `X-Event-Type: ParcelRegistered`.

```json
{
  "type": "record", "name": "ParcelRegistered", "namespace": "evals.parcels",
  "fields": [
    {"name": "reference", "type": "string"},
    {"name": "sender", "type": "string"},
    {"name": "weightGrams", "type": "int"},
    {"name": "serviceLevel", "type": "string"},
    {"name": "zone", "type": "string"},
    {"name": "source", "type": "string"},
    {"name": "registeredAt", "type": {"type": "long", "logicalType": "timestamp-millis"}}
  ]
}
```

The schema is also in `schemas/parcel-registered.avsc`. `weightGrams` is the parcel's weight in
grams, `zone` the delivery zone from the address service, `source` `api` or `manifest`.

Refused registrations publish nothing.
