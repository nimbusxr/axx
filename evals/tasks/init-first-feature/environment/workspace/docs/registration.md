# Registering parcels

Shops register parcels with `POST /api/parcels` (see `openapi.yaml`).

## A registration

- `reference`: the shop's own reference, unique across all shops. Uppercase letters, digits and
  dashes, 3 to 40 characters, starting with a letter or digit.
- `sender`: the shop's id.
- `weightGrams`: an integer from 1 to 30000. Heavier parcels need freight, not parcels.
- `serviceLevel`: `STANDARD` (default) or `EXPRESS`.
- `recipient`: `name`, `postcode` and `country` (ISO 3166-1 alpha-2, e.g. `DE`) are required;
  `street` and `city` are optional.

## What happens

1. The request is validated. Invalid requests are refused with **400** and an RFC 9457 problem
   (`application/problem+json`) whose `detail` names every invalid field.
2. A reference that is already registered is refused with **409**.
3. The recipient's postcode is checked with the address service
   (`GET /v1/postcodes/{country}/{postcode}`, authenticated with the `X-Api-Key` header).
   - Deliverable: the parcel gets the delivery `zone` the address service returned.
   - Not deliverable: the registration is refused with **422**; nothing is stored.
   - Address service down: **502**; nothing is stored.
4. The parcel is stored with status `REGISTERED` and `source` `api`, and the service answers
   **201** with the parcel and a `Location` header.
5. A `ParcelRegistered` event is published (see `events.md`).

## Reading, changing and cancelling

- `GET /api/parcels/{reference}`: the parcel, or **404**.
- `GET /api/parcels?sender=<id>`: that sender's parcels only, oldest first.
- `PATCH /api/parcels/{reference}`: change `weightGrams`, `serviceLevel` or `recipient` (same
  rules as registration) while the parcel is `REGISTERED`; **200** with the changed parcel. The
  change is stored: a later `GET` shows it.
- `DELETE /api/parcels/{reference}`: cancels the parcel, **204**. Afterwards `GET` answers **404**.
