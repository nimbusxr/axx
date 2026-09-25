---
title: Test cloud services
description: Check what your services do with S3, SQS, SNS, EventBridge, DynamoDB, Cloud Storage, Pub/Sub, BigQuery, Firestore, Blob Storage and Service Bus, against a cloud account or local emulators.
---

Services often do their work through the cloud: a file lands in a bucket and a job picks it up, results go to a warehouse, and events go to a topic. The cloud packs let a scenario do what the outside world does, such as uploading the file or publishing the event. The scenario then checks what your service did: the row, the object, the message.

```gherkin
Scenario: A claim within the limit is settled when its photo arrives
  When the evidence/crushed-box.png file is uploaded to the claim-evidence s3 bucket as claims/CLM-4101/crushed-box.png
  Then within 30s the claims dynamodb table has an item where:
    | id     | CLM-4101 |
    | status | APPROVED |
  And the refund-requests sqs queue has a message where:
    | claim            | CLM-4101 |
    | attribute reason | DAMAGED  |
```

## The packs

| Cloud | Packs |
| --- | --- |
| AWS | `aws-s3`, `aws-sqs`, `aws-sns`, `aws-eventbridge`, `aws-dynamodb`, on `aws-core` |
| Google Cloud | `gcp-storage`, `gcp-pubsub`, `gcp-bigquery`, `gcp-firestore`, on `gcp-core` |
| Azure | `azure-blob`, `azure-servicebus` |

Each pack talks to its service through the cloud's official SDK. The [step reference](/references/steps/) lists the steps of each one, grouped by cloud. With an `axx-packs.yaml` ([Choose packs](/guides/use-packs/)), list the packs you use. An AWS or Google Cloud pack brings its core with it.

## Connect as your service does

Register the account, project or resource once, usually in the `Background`. A scenario then uses it everywhere:

```gherkin
Background:
  Given the parcels aws account with the following properties:
    | region            | eu-west-1                       |
    | endpoint          | http://${sys:local.host}:4566   |
    | access key id     | ${env:AWS_ACCESS_KEY_ID:-local} |
    | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |
```

- **AWS:** `the {word} aws account` takes a `region`, and optionally an `endpoint`, a `profile` or static keys. Without keys, the SDK's default credential chain applies.
- **Google Cloud:** `the {word} gcp project` takes a `project`, and optionally an `endpoint` or a `credentials` key file. Without them, Application Default Credentials apply.
- **Azure:** each service connects to its own resource. `the {word} azure storage account` takes a `connection string` or a `url`. `the {word} service bus namespace` takes a `connection string` or a `namespace`, plus a `management endpoint` for emulators.

An `endpoint` points every service of the account or project at an emulator. Leave it out, and the same features run against the cloud.

## Run the clouds locally

Emulators such as [floci](https://floci.io) run AWS, Google Cloud and Azure on your machine: one container per cloud, free, with no account needed. Start one next to your service in the Compose file your [app definition](/guides/manage-app-lifecycle/) runs, and point your service at it the way its SDK expects:

- `AWS_ENDPOINT_URL` for AWS;
- `STORAGE_EMULATOR_HOST`, `PUBSUB_EMULATOR_HOST` and `FIRESTORE_EMULATOR_HOST` for Google Cloud;
- connection strings for Azure.

```yaml title="compose.yaml"
services:
  aws:
    image: floci/floci:latest
    ports: ['4566:4566']
    environment:
      FLOCI_HOSTNAME: aws
  claims:
    build: ../app
    environment:
      AWS_REGION: eu-west-1
      AWS_ENDPOINT_URL: http://aws:4566
```

Create the buckets, queues, topics and tables the way your infrastructure code does in a real account. Scenarios bring the data: files, seeds and messages. The examples run a one-shot `provision` command before the service starts.

## What the checks do

- **They wait.** Services act asynchronously, so every check waits: 10 seconds, or the time `within {duration}` gives.
- **They see the scenario's messages only.** A message check counts only what arrived since its scenario started. Scenarios run in parallel, so match on data unique to the scenario, such as a claim or an invoice ID.
- **Topics and buses cost nobody a message.** For the topics and buses a run checks, axx subscribes a listener of its own before the scenarios start, and removes it when the run ends:
  - an SQS queue for an SNS topic;
  - a subscription for a Pub/Sub topic;
  - a rule and a queue for an EventBridge bus;
  - a subscription for a Service Bus topic.
- **Checking a queue takes its messages.** On an SQS or Service Bus queue, axx consumes each message like any other consumer would. So check the queues your service writes to. Check a queue it consumes by what the service does with the messages.
- **Conditions compare text.** A `| field | value |` row compares the value as text, with a dotted path into nested data. `null` means null and `undefined` means absent. On messages, `attribute <name>` or `property <name>` rows check the values sent with the message.

## Examples

Each of these examples tests a service built on one cloud, with that cloud running in floci:

- [`parcel-claims`](https://github.com/nimbusxr/axx/tree/main/examples/parcel-claims), on AWS. Shops claim for damaged or lost parcels. Evidence photos in S3 trigger the decision; refunds, decisions and events go out over SQS, SNS and EventBridge.
- [`carrier-billing`](https://github.com/nimbusxr/axx/tree/main/examples/carrier-billing), on Google Cloud. Carriers' invoices uploaded to Cloud Storage are priced from BigQuery and Firestore, and disputes are published on Pub/Sub.
- [`customs-clearance`](https://github.com/nimbusxr/axx/tree/main/examples/customs-clearance), on Azure. Declarations filed on Service Bus are cleared from invoices in Blob Storage.
