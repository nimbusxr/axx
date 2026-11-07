CREATE TABLE deliveries (
    id number(10) GENERATED ALWAYS AS IDENTITY,
    parcel_id number(19) NOT NULL,
    courier_id number(19) REFERENCES drivers (id),
    attempted_at timestamp NOT NULL,
    signature varchar2(255),
    notes varchar2(400),
    PRIMARY KEY (id)
);

ALTER TABLE deliveries
    ADD CONSTRAINT deliveries_parcel_fk FOREIGN KEY (parcel_id) REFERENCES parcels (id);
