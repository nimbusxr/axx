CREATE TABLE parcels (
    id bigint IDENTITY(1, 1) CONSTRAINT pk_parcels PRIMARY KEY,
    tracking_number varchar(20) NOT NULL CONSTRAINT uq_parcels_tracking_number UNIQUE,
    depot_code varchar(10) NULL CONSTRAINT fk_parcels_depot REFERENCES depots (code),
    weight_grams int NULL,
    status varchar(20) NOT NULL CONSTRAINT df_parcels_status DEFAULT 'registered',
    label_note nvarchar(100) CONSTRAINT df_parcels_label_note DEFAULT N'fragile -- handle with care; don''t stack',
    sender_name nvarchar(100),
    row_version rowversion
);

CREATE INDEX ix_parcels_status ON parcels (status);

EXEC sp_addextendedproperty
    @name = N'MS_Description', @value = N'A parcel''s journey; from depot to door',
    @level0type = N'SCHEMA', @level0name = N'dbo',
    @level1type = N'TABLE', @level1name = N'parcels';
GO
