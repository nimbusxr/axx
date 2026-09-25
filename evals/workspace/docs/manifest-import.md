# Manifest import

Large shops don't call the API per parcel. Their systems write manifest lines straight into
the `parcels.manifest_lines` table, and the service imports them within a few seconds.

## The table

| Column | Meaning |
| --- | --- |
| `id` (text, primary key) | the line's id |
| `manifest_id` (text) | the manifest the line belongs to |
| `reference`, `sender`, `weight_grams`, `service_level` | as in an API registration |
| `recipient` (jsonb) | `{"name", "street", "city", "postcode", "country"}` |
| `status` (text) | `PENDING` when written; the importer sets `IMPORTED` or `REJECTED` |
| `error` (text) | why a line was rejected |
| `parcel_reference` (text) | the registered parcel's reference, for imported lines |
| `received_at`, `processed_at` (timestamptz) | when the line arrived and was processed |

Shops only insert `id`, `manifest_id`, `reference`, `sender`, `weight_grams`,
`service_level` and `recipient`; the other columns have defaults.

## Rules

Each pending line is registered with the same rules as the API. The outcome:

| Line | `status` | `error` |
| --- | --- | --- |
| valid | `IMPORTED` (and `parcel_reference` set) | empty |
| `weight_grams` above 30000 | `REJECTED` | `weight exceeds 30000 g` |
| `weight_grams` below 1 | `REJECTED` | `weight must be at least 1 g` |
| unknown `service_level` | `REJECTED` | `unknown service level` |
| invalid reference | `REJECTED` | `invalid reference` |
| recipient without name or postcode, or an invalid country | `REJECTED` | `invalid recipient address` |
| reference already registered | `REJECTED` | `duplicate reference` |
| address service says not deliverable | `REJECTED` | `address not deliverable` |

Rejected lines never create a parcel.

## The imported parcel

An imported line becomes a row in `parcels.parcels`:

- `reference`, `sender`, `weight_grams`, `service_level` copied from the line, `status`
  `REGISTERED`;
- `recipient` (jsonb): the line's recipient, all fields kept;
- `details` (jsonb): `{"source": "manifest", "manifestId": <manifest_id>, "lineId": <id>,
  "zone": <zone from the address service>}`.

Parcels registered through the API have `details.source` `api`.
