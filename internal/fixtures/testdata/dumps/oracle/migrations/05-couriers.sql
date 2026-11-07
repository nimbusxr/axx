RENAME drivers TO couriers;
ALTER TABLE couriers DROP COLUMN vehicle;
ALTER TABLE parcels RENAME COLUMN sender_name TO sender;
ALTER TABLE deliveries ADD (outcome varchar2(20), delivered_to varchar2(100));
ALTER TABLE deliveries DROP (signature, notes);
ALTER TABLE parcels DROP CONSTRAINT parcels_weight_positive;
DROP TABLE route_plans CASCADE CONSTRAINTS PURGE;
