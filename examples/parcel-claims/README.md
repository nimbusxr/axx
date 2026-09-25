# Parcel claims: an axx example on AWS

**Claims** is the service shops use when an insured parcel arrives damaged or never
arrives. It runs on AWS:

- **DynamoDB:** insured parcels and claims.
- **S3:** evidence photos, settlement letters, and the reviewers' copies of photos.
- **SQS, SNS and EventBridge:** everything else. S3 tells it when a photo arrives,
  carriers report the damage they caused, the parcels platform reports deliveries, and
  payments reports refunds.

axx tests it the way the rest of the world uses it: by calling its API, uploading photos,
putting carrier events on a bus, and publishing deliveries. It then checks what the
service did: the claim in DynamoDB, the letter in S3, the refund request on a queue, and
the decision on a topic and a bus. The service knows nothing about axx.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `damage-claims` | A claim within the auto-approval limit is settled when its photo arrives. A larger one goes to a person, with the photo copied for the reviewer. A parcel is claimed once. | REST with OpenAPI, S3 uploads and objects, DynamoDB items and counts, SQS, SNS and EventBridge checks |
| `lost-claims` | A lost parcel is paid out unless the parcels platform reported it delivered. A parcel event without a type is set aside. | publishing to SNS, with and without attributes, and a log entry that proves an event was ignored |
| `carrier-reports` | A carrier's damage report settles the claim without a photo. | putting an event on an EventBridge bus |
| `refunds` | Paid refunds close claims. Failed ones are flagged with the reason payments gives. | sending to SQS, with and without attributes |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` works through these steps:

1. It runs `docker compose up --build` in `../infra`, which starts AWS locally.
2. It creates the service's tables, buckets, queues, topics, buses, notifications and
   rules, then starts the service.
3. It waits for `http://localhost:8500/health`.
4. It runs the features in parallel.
5. It removes everything with `docker compose down -v`.

## AWS, locally

AWS runs in [floci](https://floci.io), a free, open-source AWS emulator. It's one
container, with every service on port 4566.

The service is set up exactly as it would be for AWS: the SDK's usual configuration, with
`AWS_ENDPOINT_URL` pointing it at the emulator. `claims provision` creates what the
infrastructure code of a real account creates.

The features register the account the same way:

```gherkin
Given the parcels aws account with the following properties:
  | region            | eu-west-1                           |
  | endpoint          | http://${sys:local.host}:4566       |
  | access key id     | ${env:AWS_ACCESS_KEY_ID:-local}     |
  | secret access key | ${env:AWS_SECRET_ACCESS_KEY:-local} |
```

To run the same features against an AWS account, drop the `endpoint` row and let the
credentials come from your environment.

| Resource | Kind | Purpose |
| --- | --- | --- |
| `insured-parcels`, `claims` | DynamoDB tables | insured parcels (seeded by the scenarios) and claims |
| `claim-evidence` | S3 bucket | shops' photos; new objects notify the `evidence-uploads` queue |
| `claim-letters`, `claim-reviews` | S3 buckets | settlement letters; photos copied for reviewers |
| `refund-requests` | SQS queue | refunds for payments to pay (the service writes it; axx checks it) |
| `refund-results` | SQS queue | payments' answers (axx sends them; the service reads them) |
| `claim-decisions` | SNS topic | decisions, for the shops |
| `parcel-events` | SNS topic | the parcels platform's events, delivered raw to `claims-parcel-events` |
| `parcels`, `carrier-events` | EventBridge buses | the claims' events; carriers' damage reports, routed to `carrier-damage-reports` |
