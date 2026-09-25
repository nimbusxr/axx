# Tracking

Depot scanners write every scan into the MongoDB collection `scans` (database `parcels`):

```json
{"scanId": "S-1001", "parcelRef": "PX-1", "status": "IN_TRANSIT", "location": "Leipzig hub",
 "scannedAt": {"$date": "2026-09-01T10:00:00Z"}}
```

`scannedAt` is a date (a `{"$date": ...}` value in Extended JSON; ISO-8601 strings are accepted
too). Scanners work offline and upload in batches, so scans arrive **out of order**, and a
scanner that loses its connection **resends** scans it already sent (same `scanId`).

## The tracking view

Within a few seconds of a scan arriving, the service updates the parcel's tracking view: one
document per parcel in the collection `tracking` (`_id` is the parcel reference):

| Field | Value |
| --- | --- |
| `parcelRef` | the parcel reference |
| `status` | the status of the **latest** scan by `scannedAt` (not the last one received) |
| `lastLocation` | the location of that latest scan |
| `lastScanAt` | its `scannedAt` |
| `scanCount` | the number of **distinct** scans (a resent scan counts once) |
| `delivered` | `true` once any scan has status `DELIVERED` |

`GET /api/parcels/{reference}/tracking` serves the same view.
