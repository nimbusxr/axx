CREATE TABLE deliveries (
    id integer NOT NULL GENERATED ALWAYS AS IDENTITY,
    parcel_id bigint NOT NULL,
    courier_id bigint,
    attempted_at timestamp NOT NULL,
    signature varchar(255),
    notes varchar(400),
    PRIMARY KEY (id),
    CONSTRAINT deliveries_courier_fk FOREIGN KEY (courier_id) REFERENCES drivers (id)
);

ALTER TABLE deliveries
    ADD CONSTRAINT deliveries_parcel_fk FOREIGN KEY (parcel_id) REFERENCES parcels (id);
