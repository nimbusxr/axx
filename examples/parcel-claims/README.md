# Parcel claims: an axx example on AWS

**Claims** is the service shops use when an insured parcel arrives damaged or never
arrives. It runs on AWS:

- **DynamoDB:** insured parcels and claims.
- **S3:** evidence photos, settlement letters, the settlements for finance, and the
  reviewers' copies of photos.
- **SQS, SNS and EventBridge:** everything else. S3 tells it when a photo arrives,
  carriers report the damage they caused, the parcels platform reports deliveries, and
  payments reports refunds.
- **Models of its own:** before it settles a claim without a person, it checks that the
  photo shows a parcel, with an image classifier (MobileNetV2) that it runs itself, with
  ONNX Runtime; a small language model (SmolLM2-360M-Instruct), which it runs with
  llama.cpp, reads each damage claim's description; and a speech recognizer (Whisper
  base.en), which it runs with whisper.cpp, transcribes the customers' voice notes.

axx tests it the way the rest of the world uses it: by calling its API, uploading photos,
putting carrier events on a bus, and publishing deliveries. It then checks what the
service did: the claim in DynamoDB, the letter and the settlement in S3, the refund request
on a queue, and the decision on a topic and a bus. The service knows nothing about axx.

## What the features check

| Feature | Acceptance criteria | What axx uses |
| --- | --- | --- |
| `damage-claims` | A damage claim waits for its photo. A claim within the auto-approval limit is settled when its photo arrives and shows a parcel. A larger one, or a photo that shows no parcel, goes to a person, with the photo copied for the reviewer. A claim whose description reports no damage in transit goes to a person when it is filed. A customer's voice note is the claim's description, and one that reports no damage in transit sends the claim to a person. A parcel is claimed once. | REST with OpenAPI, S3 uploads and objects (JSON properties, a letter's text, identical to a file), DynamoDB items and counts, SQS, SNS and EventBridge checks, stubs of the photo check's model, the triage's language model and the voice notes' speech recognizer |
| `lost-claims` | A lost parcel is paid out unless the parcels platform reported it delivered. A parcel event without a type is set aside. | publishing to SNS, with and without attributes, a CSV row in an S3 object, and a log entry that proves an event was ignored |
| `carrier-reports` | A carrier's damage report settles the claim without a photo. | putting an event on an EventBridge bus |
| `refunds` | Paid refunds close claims. Failed ones are flagged with the reason payments gives. | sending to SQS, with and without attributes |

## Run it

You need Docker with Compose v2, and axx. From `acceptance/`:

```sh
cd acceptance
axx run
```

`axx run` works through these steps:

1. It builds the voice notes' stub (`axx models build`), then runs `docker compose up --build`
   in `../infra`, which starts AWS locally.
2. It creates the service's tables, buckets, queues, topics, buses, notifications and
   rules, then starts the service.
3. It waits for `http://localhost:8500/health`.
4. It runs the features in parallel.
5. It removes everything with `docker compose down -v`.

## The photo check's model, stubbed

The service runs an image classifier itself: MobileNetV2, 14 MB, which it loads with ONNX
Runtime. Tests do not need it. `models/evidence-check` is a stub of it: the model's
contract (`contract.json`, written from the real model with `axx models contract`) and
mappings from the evidence photos the scenarios upload to what the model sees in them (a
carton, a packet, a web site). `axx models build` writes the stub
(`infra/models/evidence-check.onnx`, a 30 KB model of the real one's inputs and outputs;
`go generate ./...`, in axx's repository, builds it again), and the compose file gives it to
the service in place of the real model. See
[Stub embedded models](https://axx.nimbusxr.us/guides/stub-embedded-models/).

## The triage's language model, stubbed

A shop describes the damage when it files a claim, and a language model the service runs
itself reads the description: whether it reports damage in transit, and what it says, in a
sentence, as JSON a grammar keeps it to. The model is SmolLM2-360M-Instruct, 270 MB, which
the service runs with llama.cpp (the image builds llama.cpp and links it in). Tests do not
need it either. `models/description-triage` is a stub of it: the model's contract
(`contract.json`, written from the real model with `axx models contract`, and its tokenizer
and chat template, in `model-files`) and mappings from the descriptions the scenarios file to
what the model answers. `axx models build` writes the stub (`infra/models/description-triage.gguf`,
6.7 MB, a model of the real one's tokenizer and chat template), and the compose file gives it to
the service in place of the real model.

## The voice notes' speech recognizer, stubbed

A customer can describe the damage on the phone instead: the shop uploads the recording
with the claim's evidence (`claims/<claim>/voice-note.wav`, at 16 kHz), and the service
transcribes it with a speech recognizer it runs itself, Whisper base.en
(`ggml-base.en.bin`, 148 MB), with whisper.cpp (the image builds it from its release, on
llama.cpp's ggml, and links it in, as it does llama.cpp). The transcript is the claim's description, and the triage
reads it as it reads a typed one. Tests do not need the real model: `models/voice-notes`
is a stub of it, the model's contract (`contract.json`, written from the real model with
`axx models contract`, its mel filters and vocabulary in `model-files`) and mappings from
the voice notes the scenarios upload (`features/damage-claims/evidence`, recorded with
espeak-ng) to what the model hears in them. A stub of Whisper is the real model's size,
so the example does not keep it: `axx models build` writes it as the example starts
(`acceptance/axx.yaml`), to `infra/models/voice-notes.bin`, and the compose file gives it
to the service in place of the real model.

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
| `claim-evidence` | S3 bucket | shops' photos and voice notes; new objects notify the `evidence-uploads` queue |
| `claim-letters`, `claim-reviews` | S3 buckets | settlement letters, as JSON for the shop's systems and text for its staff; photos copied for reviewers |
| `claim-settlements` | S3 bucket | the settlement of every approved claim, as CSV for finance to book |
| `refund-requests` | SQS queue | refunds for payments to pay (the service writes it; axx checks it) |
| `refund-results` | SQS queue | payments' answers (axx sends them; the service reads them) |
| `claim-decisions` | SNS topic | decisions, for the shops |
| `parcel-events` | SNS topic | the parcels platform's events, delivered raw to `claims-parcel-events` |
| `parcels`, `carrier-events` | EventBridge buses | the claims' events; carriers' damage reports, routed to `carrier-damage-reports` |
