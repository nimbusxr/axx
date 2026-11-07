ALTER TABLE drivers RENAME TO couriers;
ALTER TABLE couriers DROP COLUMN vehicle;
ALTER TABLE parcels RENAME COLUMN sender_name TO sender;
ALTER TABLE deliveries ADD COLUMN outcome TEXT;
ALTER TABLE deliveries DROP COLUMN signature;
DROP TABLE IF EXISTS route_plans;
