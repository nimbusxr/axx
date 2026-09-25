Big shops don't call our API for every parcel: their systems write manifest lines straight
into the `parcels.manifest_lines` table, and the parcels service imports them within a few
seconds (`docs/manifest-import.md` has the details). There are no acceptance tests for the
import yet. Please add them to this repository (see `README.md` for the service and how the
tests run).

Acceptance criteria:

1. A valid manifest line becomes a registered parcel: its sender, weight and service level
   are copied, the full recipient address is kept, and the parcel records where it came
   from (source `manifest`, the manifest id and the line id).
2. Once imported, the manifest line is marked `IMPORTED` and points to the parcel it created.
3. A line for a parcel heavier than 30000 g is marked `REJECTED` with the reason
   `weight exceeds 30000 g`, and no parcel is created for it.

The tests must pass against the service as it is today, and fail if any of these behaviors
breaks. Put the features in `features/` and the rows you insert in seed files in `seeds/`;
don't change anything else in the repository.
