-- Fill in the weights the depots scanned, then require one.
CREATE TEMPORARY TABLE scanned_weights (
    tracking_number varchar(20) NOT NULL,
    weight_grams integer NOT NULL
) ON COMMIT DROP;

UPDATE parcels p SET weight_grams = s.weight_grams
FROM scanned_weights s WHERE s.tracking_number = p.tracking_number;

ALTER TABLE parcels ALTER COLUMN weight_grams SET NOT NULL;
ALTER TABLE parcels ADD CONSTRAINT parcels_weight_positive CHECK (weight_grams > 0);

-- A parcel's status is now always given by the depot that registers it.
ALTER TABLE parcels ALTER COLUMN status DROP DEFAULT;

ALTER TABLE deliveries ALTER attempted_at SET DEFAULT now();
ALTER TABLE ONLY drivers ALTER COLUMN phone SET DATA TYPE varchar(32);
