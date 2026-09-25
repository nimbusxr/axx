# Shipping labels

`GET /api/parcels/{reference}/label` returns the parcel's label:

```json
{"reference": "PX-1", "serviceLevel": "STANDARD", "barcode": "PX01000000016",
 "signature": "8f7d9483..."}
```

## Barcode

`PX`, then the parcel's 10-digit label number, then **one check digit**: the Luhn (mod 10)
check digit computed over the 10 digits of the label number. Depot scanners reject barcodes
whose check digit is wrong.

The label number is the `label_number` column of `parcels.parcels`, assigned from a sequence
when the parcel is stored (it can also be given explicitly, e.g. in a seed).

## Signature

`signature` is the lowercase hex HMAC-SHA256 of `<reference>|<barcode>` (the reference, a
pipe, the barcode) with the label signing key. In the test environment the key is
`evals-label-secret`. Scanners refuse labels whose signature does not verify.
