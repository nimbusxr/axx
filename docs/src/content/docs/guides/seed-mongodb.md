---
title: Seed and query MongoDB
description: Register a MongoDB database, insert documents from JSON seed files, and assert on the documents your service wrote.
---

The MongoDB steps put documents in place before your service reads them, and check the documents it writes.

## Register the database

```gherkin
Background:
  Given a tracking-db mongo database with the following properties:
    | url      | mongodb://localhost:27017/parcels?authSource=admin |
    | user     | parcels                                            |
    | password | parcels                                            |
```

All three properties are required and support `${env:...}` and `${sys:...}`. The URL must include the database name; `authSource` defaults to it. The first MongoDB database registered in a scenario is the default.

## Write a seed file

A seed is a JSON object that maps collection names to arrays of documents. Extended JSON (`{"$oid": "..."}`, `{"$date": "..."}`) is supported:

```json title="seeds/scans-in-transit.json"
{
  "scans": [
    {
      "scanId": "SC-3001-2",
      "parcelRef": "PX-TRK-3001",
      "status": "IN_TRANSIT",
      "location": "Hamburg hub",
      "scannedAt": {"$date": "2026-05-05T06:40:00Z"}
    },
    {
      "scanId": "SC-3001-1",
      "parcelRef": "PX-TRK-3001",
      "status": "PICKED_UP",
      "location": "Berlin depot",
      "scannedAt": {"$date": "2026-05-04T16:10:00Z"}
    }
  ]
}
```

## Seed and use it

```gherkin
Scenario: Tracking shows the latest scan, however the scans arrive
  Given a seeds/scans-in-transit.json mongo db seed
  And within 10s a selection of at least 1 document is retrieved from the tracking collection where:
    | _id       | PX-TRK-3001 |
    | scanCount | 2           |
  When a GET request to /api/parcels/PX-TRK-3001/tracking
  And the request is executed
  Then the response status code is 200
  And the response payload properties are:
    | status       | IN_TRANSIT  |
    | lastLocation | Hamburg hub |
    | scanCount    | 2           |
```

The service builds its tracking summaries from the scans in the background, so the scenario [waits for the summary](#wait-for-asynchronous-writes) before it calls the API.

To seed a specific database when the scenario registers more than one:

```gherkin
Given a seeds/scans-in-transit.json MongoDB seed for tracking-db
```

`a seeds/scans-in-transit.json mongo db seed for tracking-db` is the same step with the other spelling.

The file path resolves against the `resources` directories in `axx.yaml`. See the [mongo step reference](/references/steps/mongo/).

## Query and assert

A document selection finds documents with equality conditions. Dotted field paths reach into nested documents, and [the step reference](/references/steps/mongo/#mongofind) says how values are read:

```gherkin
Then a selection of documents is retrieved from the scans collection where:
  | parcelRef | PX-TRK-3001 |
  | status    | PICKED_UP   |
And the selection has 1 document
And the 1st document for the selection properties are:
  | scanId    | SC-3001-1                |
  | location  | Berlin depot             |
  | scannedAt | 2026-05-04T16:10:00.000Z |
```

Properties are JSONPaths (`location`, `history[0].status`), compared as text; `null` means null and `undefined` means the field is absent ([the rules](/references/steps/mongo/#mongodocare)). `the 1st document for the selection properties match:` takes Java regular expressions instead.

Like the SQL selections, they are numbered in the order they are retrieved (`the 2nd selection has 3 documents`), and `has more than` / `has fewer than` compare counts.

### Wait for asynchronous writes

```gherkin
Then within 10s a selection of at least 1 document is retrieved from the tracking collection where:
  | _id       | PX-TRK-3001 |
  | scanCount | 2           |
```

The step polls every 500 ms until enough documents match or the time runs out. Values that parse as JSON are typed, so `2` matches the number 2.

Every query step has an `on <service>` form for scenarios with more than one MongoDB database.

## Keep documents apart

Seeded documents stay in the database after the scenario, and scenarios run in parallel. Give each scenario's documents their own keys (`PX-TRK-3001` above) and query by them. [Scenario isolation](/explanations/scenario-isolation/) explains the approach.
