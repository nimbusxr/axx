EXEC sp_rename 'drivers', 'couriers';
ALTER TABLE couriers DROP COLUMN vehicle;
EXEC sp_rename 'dbo.parcels.sender_name', 'sender', 'COLUMN';
ALTER TABLE parcels ADD weight_kg AS (weight_grams / 1000.0) PERSISTED, [order] varchar(32) NULL;
ALTER TABLE deliveries ADD outcome nvarchar(20) NULL;
ALTER TABLE deliveries DROP COLUMN signature;
ALTER TABLE parcels DROP CONSTRAINT ck_parcels_weight_positive;
DROP TABLE IF EXISTS route_plans;
GO
