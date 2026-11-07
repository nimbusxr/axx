-- Fill in the weights the depots scanned, then require one.
CREATE TABLE #scanned_weights (
    tracking_number varchar(20) NOT NULL,
    weight_grams int NOT NULL
);
UPDATE p SET p.weight_grams = s.weight_grams
FROM parcels p JOIN #scanned_weights s ON s.tracking_number = p.tracking_number;
DROP TABLE #scanned_weights;

ALTER TABLE parcels ALTER COLUMN weight_grams int NOT NULL;
ALTER TABLE parcels ADD CONSTRAINT ck_parcels_weight_positive CHECK (weight_grams > 0);

/* A parcel's status is now always given by the depot that registers it. */
ALTER TABLE parcels DROP CONSTRAINT df_parcels_status;

ALTER TABLE deliveries ADD CONSTRAINT df_deliveries_attempted_at DEFAULT (sysutcdatetime()) FOR attempted_at;
ALTER TABLE drivers ALTER COLUMN phone varchar(32) NULL;
GO
