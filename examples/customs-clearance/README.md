# Customs clearance: an axx example on Azure

**Customs** clears the parcels the company ships abroad. It runs on Azure:

- **Filing:** brokers upload a parcel's commercial invoice to Blob Storage and file its
  declaration on a Service Bus queue.
- **Low-value parcels:** a parcel worth up to 150 EUR is cleared. The service writes a
  clearance certificate (JSON, and text to print), archives the invoice, and announces the
  decision on a Service Bus topic.
- **Parcels that owe duties:** the service works out the duties on every line of the
  invoice and archives the breakdown. It sends a payment request for the duties on a
  queue, and the parcel is held.
- **At the border:** carriers announce parcels reaching the border on another topic, and
  the service releases the cleared ones for delivery.
- **A model of its own:** an officer may open a parcel at the border and photograph its
  contents. The service holds a cleared parcel whose photo shows goods its invoice does not
  declare: an image classifier (MobileNetV3 Small), which the service runs itself with
  OpenVINO, names what the photo shows.

axx tests it the way brokers and carriers use it: uploading invoices, filing declarations,
and announcing arrivals. It then checks the certificates and the archive in Blob Storage,
the duty requests on their queue, and the events on the topic.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `declarations` | A low-value parcel is cleared. A parcel above the de minimis value owes duties. A declaration without its invoice is held. | Blob Storage uploads and blobs (JSON properties, a certificate's text, identical to a file, a CSV row), Service Bus queue sends (with application properties) and checks, topic checks |
| `border` | A cleared parcel is released when it reaches the border. One that is not is held. An inspected parcel is released when its contents match its invoice, and held when they differ. | sending to a Service Bus topic, Blob Storage uploads under another name, a stub of the contents check's model |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` starts Azure and the service with `docker compose up --build` in `../infra`,
waits for `http://localhost:8700/health`, runs the features, and removes everything with
`docker compose down -v`.

## The contents check's model, stubbed

The service checks an inspection's photo (a blob in the `inspections` container, which the
carrier's border event names) with an image classifier it runs itself, MobileNetV3 Small as
OpenVINO publishes it (its IR, 5 MB), with OpenVINO (its release's C library, which the
image links in). The invoice's tariff codes say what the photo may show: a parcel is held when the
model is sure it shows goods of a chapter the invoice does not declare. Tests do not need
the real model: `models/contents-check` is a stub of it, the model's contract
(`contract.json`, written from the real model with `axx models contract`) and mappings from
the photos the scenarios upload to what the model sees in them (a teddy bear, a phone).
`axx models build` writes the stub (`infra/models/contents-check/model.xml` and
`model.bin`, 31 KB; `go generate ./...`, in axx's repository, builds it again), and the
compose file gives it to the service in place of the real model.

## Azure, locally

Azure runs in two emulators:

- **Blob Storage** in [floci](https://floci.io) (`floci-az`), a free, open-source emulator,
  on port 4577.
- **Service Bus** in Microsoft's
  [Service Bus emulator](https://learn.microsoft.com/azure/service-bus-messaging/overview-emulator),
  on ports 5672 (messaging) and 5300 (management), with the Azure SQL Edge it keeps its
  state in. Running it accepts the emulator's and Azure SQL Edge's license terms
  (`ACCEPT_EULA` in `infra/compose.yaml`).

The service is set up exactly as it would be for Azure, from connection strings. The only
emulator-specific setting is `SERVICEBUS_MANAGEMENT_ENDPOINT`, because the emulator's
management API isn't on the namespace's host. `customs provision` creates what the
infrastructure code of a real subscription creates.

The features register the storage account and the namespace:

```gherkin
Given the customs azure storage account with the following properties:
  | connection string | DefaultEndpointsProtocol=http;AccountName=devstoreaccount1;AccountKey=bG9jYWw=;BlobEndpoint=http://${sys:local.host}:4577/devstoreaccount1; |
And the customs service bus namespace with the following properties:
  | connection string   | Endpoint=sb://${sys:local.host}:5672;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true; |
  | management endpoint | http://${sys:local.host}:5300                                                                                                                 |
```

To run the same features against Azure, use the account's and namespace's connection
strings (from `${env:...}`) and drop the `management endpoint` row.

| Resource | Kind | Purpose |
| --- | --- | --- |
| `commercial-invoices` | blob container | brokers' invoices, `<declaration>.json` |
| `inspections` | blob container | photos of inspected parcels' contents, `<declaration>.png` |
| `clearances`, `customs-archive` | blob containers | clearance certificates (`<declaration>.json`, and `.txt` to print); archived invoices and duty breakdowns (`<declaration>/invoice.json`, `<declaration>/duties.csv`) |
| `customs-filings` | Service Bus queue | brokers' declarations (axx sends them; the service reads them) |
| `duty-payments` | Service Bus queue | duties for payments to collect (the service writes it; axx checks it) |
| `customs-events` | Service Bus topic | `DeclarationCleared`, `DeclarationHeld`, `ReleasedForDelivery`, `HeldAtBorder` |
| `border-events` | Service Bus topic | carriers' arrivals, read through the `customs` subscription |
