# Customs clearance: an axx example on Azure

**Customs** clears the parcels the company ships abroad. It runs on Azure:

- **Filing:** brokers upload a parcel's commercial invoice to Blob Storage and file its
  declaration on a Service Bus queue.
- **Low-value parcels:** a parcel worth up to 150 EUR is cleared. The service writes a
  clearance certificate, archives the invoice, and announces the decision on a Service
  Bus topic.
- **Parcels that owe duties:** the service sends a payment request for the duties on a
  queue, and the parcel is held.
- **At the border:** carriers announce parcels reaching the border on another topic, and
  the service releases the cleared ones for delivery.

axx tests it the way brokers and carriers use it: uploading invoices, filing declarations,
and announcing arrivals. It then checks the certificates and the archive in Blob Storage,
the duty requests on their queue, and the events on the topic.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `declarations` | A low-value parcel is cleared. A parcel above the de minimis value owes duties. A declaration without its invoice is held. | Blob Storage uploads and blobs (JSON properties, identical to a file), Service Bus queue sends (with application properties) and checks, topic checks |
| `border` | A cleared parcel is released when it reaches the border. One that is not is held. | sending to a Service Bus topic |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` starts Azure and the service with `docker compose up --build` in `../infra`,
waits for `http://localhost:8700/health`, runs the features, and removes everything with
`docker compose down -v`.

## Azure, locally

Azure runs in [floci](https://floci.io) (`floci-az`), a free, open-source emulator:

- **Blob Storage and management:** Blob Storage and the Service Bus management API are
  on port 4577.
- **Service Bus messaging:** floci-az starts a broker container of its own on the host's
  port 5673. That needs the Docker socket and `FLOCI_AZ_SERVICES_SERVICE_BUS_MOCKED=false`.

The service is set up exactly as it would be for Azure, from connection strings. The only
emulator-specific setting is `SERVICEBUS_MANAGEMENT_ENDPOINT`, because the emulator's
management API isn't on the namespace's host. `customs provision` creates what the
infrastructure code of a real subscription creates.

The features register the storage account and the namespace:

```gherkin
Given the customs azure storage account with the following properties:
  | connection string | DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=bG9jYWw=;BlobEndpoint=http://${sys:local.host}:4577/devstoreaccount1; |
And the customs service bus namespace with the following properties:
  | connection string   | Endpoint=sb://${sys:local.host}:5673;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=local;UseDevelopmentEmulator=true; |
  | management endpoint | http://${sys:local.host}:4577/devstoreaccount1-servicebus |
```

To run the same features against Azure, use the account's and namespace's connection
strings (from `${env:...}`) and drop the `management endpoint` row.

| Resource | Kind | Purpose |
| --- | --- | --- |
| `commercial-invoices` | blob container | brokers' invoices, `<declaration>.json` |
| `clearances`, `customs-archive` | blob containers | clearance certificates; archived invoices |
| `customs-filings` | Service Bus queue | brokers' declarations (axx sends them; the service reads them) |
| `duty-payments` | Service Bus queue | duties for payments to collect (the service writes it; axx checks it) |
| `customs-events` | Service Bus topic | `DeclarationCleared`, `DeclarationHeld`, `ReleasedForDelivery`, `HeldAtBorder` |
| `border-events` | Service Bus topic | carriers' arrivals, read through the `customs` subscription |
