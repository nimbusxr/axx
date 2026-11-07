CREATE TABLE deliveries (
    id int IDENTITY(1, 1) NOT NULL,
    parcel_id bigint NOT NULL,
    courier_id bigint NULL REFERENCES drivers (id),
    attempted_at datetime2 NOT NULL,
    signature nvarchar(255),
    CONSTRAINT pk_deliveries PRIMARY KEY CLUSTERED (id)
);

ALTER TABLE deliveries WITH CHECK
    ADD CONSTRAINT fk_deliveries_parcel FOREIGN KEY (parcel_id) REFERENCES parcels (id);
GO
