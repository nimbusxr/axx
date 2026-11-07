# Fill in the weights the depots scanned, then require one.
CREATE TEMPORARY TABLE scanned_weights (
    tracking_number varchar(20) NOT NULL,
    weight_grams int NOT NULL
);
UPDATE parcels p JOIN scanned_weights s ON s.tracking_number = p.tracking_number
SET p.weight_grams = s.weight_grams;
DROP TEMPORARY TABLE scanned_weights;

ALTER TABLE parcels MODIFY COLUMN weight_grams int NOT NULL;
ALTER TABLE parcels ADD CONSTRAINT parcels_weight_positive CHECK (weight_grams > 0);

/* A parcel's status is now always given by the depot that registers it. */
ALTER TABLE parcels ALTER COLUMN status DROP DEFAULT;

ALTER TABLE deliveries ALTER attempted_at SET DEFAULT (now());
ALTER TABLE drivers MODIFY depot_code varchar(10) AFTER phone;
ALTER TABLE depots ADD COLUMN region char(2) NOT NULL FIRST;
