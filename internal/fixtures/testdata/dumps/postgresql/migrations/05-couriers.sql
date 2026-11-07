ALTER TABLE drivers RENAME TO couriers;
ALTER TABLE couriers DROP COLUMN vehicle;
ALTER TABLE parcels RENAME COLUMN sender_name TO sender;
ALTER TABLE deliveries ADD COLUMN outcome text, DROP COLUMN IF EXISTS signature;
ALTER TABLE parcels DROP CONSTRAINT parcels_weight_positive;
DROP TABLE IF EXISTS route_plans;
