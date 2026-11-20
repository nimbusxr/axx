-- Fill in the weights the depots scanned, then require one.
CREATE TABLE scanned_weights (
    tracking_number varchar(20) NOT NULL,
    weight_grams integer
);

UPDATE parcels p
SET weight_grams = (SELECT s.weight_grams FROM scanned_weights s WHERE s.tracking_number = p.tracking_number)
WHERE EXISTS (SELECT 1 FROM scanned_weights s WHERE s.tracking_number = p.tracking_number);

DROP TABLE scanned_weights;

ALTER TABLE parcels ALTER COLUMN weight_grams SET NOT NULL;
-- Db2 asks for a reorganization after a change like that one, before the table is written again.
CALL SYSPROC.ADMIN_CMD('REORG TABLE parcels');
ALTER TABLE parcels ADD CONSTRAINT parcels_weight_positive CHECK (weight_grams > 0);

/* A parcel's status is now always given by the depot that registers it. */
ALTER TABLE parcels ALTER COLUMN status DROP DEFAULT;

ALTER TABLE deliveries ALTER COLUMN attempted_at SET DEFAULT CURRENT TIMESTAMP;
ALTER TABLE drivers ALTER COLUMN phone SET DATA TYPE varchar(32);
