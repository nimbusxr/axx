CREATE TABLE deliveries (
    id int NOT NULL AUTO_INCREMENT PRIMARY KEY,
    parcel_id bigint unsigned NOT NULL,
    courier_id bigint,
    attempted_at datetime NOT NULL,
    signature varchar(255),
    FOREIGN KEY (courier_id) REFERENCES drivers (id)
);

ALTER TABLE deliveries
    ADD CONSTRAINT deliveries_parcel FOREIGN KEY (parcel_id) REFERENCES parcels (id);
