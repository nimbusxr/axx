-- Fill in the weights the depots scanned, then require one.
CREATE PRIVATE TEMPORARY TABLE ora$ptt_scanned_weights (
    tracking_number varchar2(20),
    weight_grams number(10)
) ON COMMIT DROP DEFINITION;

UPDATE parcels p
SET weight_grams = (SELECT s.weight_grams FROM ora$ptt_scanned_weights s WHERE s.tracking_number = p.tracking_number)
WHERE EXISTS (SELECT 1 FROM ora$ptt_scanned_weights s WHERE s.tracking_number = p.tracking_number);

ALTER TABLE parcels MODIFY (weight_grams NOT NULL);
ALTER TABLE parcels ADD CONSTRAINT parcels_weight_positive CHECK (weight_grams > 0);

/* A parcel's status is now always given by the depot that registers it. */
ALTER TABLE parcels MODIFY status DEFAULT NULL;

ALTER TABLE deliveries MODIFY (attempted_at DEFAULT SYSTIMESTAMP);
ALTER TABLE drivers MODIFY (phone varchar2(32));
