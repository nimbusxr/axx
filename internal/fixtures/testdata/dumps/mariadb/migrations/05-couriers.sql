RENAME TABLE drivers TO couriers;
ALTER TABLE couriers DROP COLUMN vehicle;
ALTER TABLE parcels CHANGE COLUMN sender_name sender varchar(100);
ALTER TABLE deliveries
    ADD COLUMN (outcome varchar(20) NOT NULL, proof_photo varchar(255)),
    DROP COLUMN signature;
ALTER TABLE deliveries ALTER COLUMN outcome SET DEFAULT 'pending';
ALTER TABLE parcels DROP CONSTRAINT parcels_weight_positive, DROP INDEX parcels_status;
ALTER TABLE route_plans DROP FOREIGN KEY route_plans_ibfk_1;
DROP TABLE IF EXISTS route_plans;
