# Carrier billing: an axx example on Google Cloud

**Billing** reconciles the invoices carriers send against the rates agreed with them. It
runs on Google Cloud:

- **Cloud Storage:** carriers upload invoices to a bucket. A new invoice is announced on a
  Pub/Sub topic, and the service reconciles it.
- **Firestore and BigQuery:** the service prices every line at the carrier's rate from
  BigQuery, for the weight the parcels platform measured (kept in Firestore). It writes
  the priced lines to BigQuery and the invoice's summary to Firestore.
- **Disputes:** overcharged lines and parcels nobody shipped are written to a dispute file
  in Cloud Storage.
- **Pub/Sub:** the outcome is announced on a topic, and the parcels platform's weights
  arrive on another.

axx tests it the way the rest of the world uses it: carriers uploading invoices, and the
parcels platform publishing weights. It then checks the rows in BigQuery, the documents in
Firestore, the files in Cloud Storage, the events on Pub/Sub, and what the service logged.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `invoices` | An invoice at the agreed rates is reconciled. Overcharged lines and unknown parcels are disputed. An invoice uploaded twice is reconciled once. | Cloud Storage uploads, objects (identical to a file, JSON properties), BigQuery seeds, rows and counts, Firestore seeds and documents, Pub/Sub checks, a log entry |
| `shipments` | A weighed parcel is kept for pricing. A shipment event without a type is set aside. | publishing to Pub/Sub, with and without attributes, a Firestore collection query, a log entry |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` starts Google Cloud and the service with `docker compose up --build` in
`../infra`, waits for `http://localhost:8600/health`, runs the features, and removes
everything with `docker compose down -v`.

## Google Cloud, locally

Google Cloud runs in [floci](https://floci.io) (`floci-gcp`), a free, open-source
emulator. It's one container, with every service on port 4588.

The service is set up exactly as it would be for Google Cloud: Application Default
Credentials, plus the usual emulator variables (`STORAGE_EMULATOR_HOST`,
`PUBSUB_EMULATOR_HOST`, `FIRESTORE_EMULATOR_HOST`). BigQuery has no such variable, so
`BIGQUERY_ENDPOINT` stands in for it. `billing provision` creates what the infrastructure
code of a real project creates.

The features register the project:

```gherkin
Given the billing gcp project with the following properties:
  | project  | parcels-billing               |
  | endpoint | http://${sys:local.host}:4588 |
```

To run the same features against a Google Cloud project, give its ID and drop the
`endpoint` row. The clients then use Application Default Credentials.

One difference from Google Cloud: floci does not publish a bucket's notifications when
the topic is registered by its full resource name (`//pubsub.googleapis.com/...`), as
Cloud Storage documents it. So against an emulator, `billing provision` registers it by its
short name (see `notify` in `app/provision.go`).

| Resource | Kind | Purpose |
| --- | --- | --- |
| `carrier-invoices` | Cloud Storage bucket | carriers' invoices, `<carrier>/<invoice>.csv`; new ones are announced on `invoice-uploads` |
| `billing-disputes` | Cloud Storage bucket | the disputed lines of an invoice (CSV) and a summary (JSON) |
| `invoice-uploads`, `shipment-events` | Pub/Sub topics | read by the service's subscriptions |
| `invoice-events` | Pub/Sub topic | `InvoiceReconciled` and `InvoiceDisputed` |
| `billing.carrier_rates`, `billing.invoice_lines` | BigQuery tables | agreed prices per kg; every priced line |
| `shipments`, `invoices` | Firestore collections | measured weights; invoice summaries |
