-- Db2 renames no table that has a foreign key or is named by one: the keys go, and come back.
ALTER TABLE deliveries DROP FOREIGN KEY deliveries_courier_fk;
ALTER TABLE drivers DROP CONSTRAINT drivers_depot_fk;
RENAME TABLE drivers TO couriers;
ALTER TABLE couriers
    ADD CONSTRAINT couriers_depot_fk FOREIGN KEY (depot_code) REFERENCES depots (code);
ALTER TABLE deliveries
    ADD CONSTRAINT deliveries_courier_fk FOREIGN KEY (courier_id) REFERENCES couriers (id);
ALTER TABLE couriers DROP COLUMN vehicle;
CALL SYSPROC.ADMIN_CMD('REORG TABLE couriers');
ALTER TABLE parcels RENAME COLUMN sender_name TO sender;
ALTER TABLE deliveries ADD COLUMN outcome varchar(20) ADD COLUMN delivered_to varchar(100);
ALTER TABLE deliveries DROP COLUMN signature DROP COLUMN notes;
CALL SYSPROC.ADMIN_CMD('REORG TABLE deliveries');
ALTER TABLE parcels DROP CONSTRAINT parcels_weight_positive;
DROP TABLE route_plans;
