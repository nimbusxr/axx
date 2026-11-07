CREATE TABLE [dbo].[deliveries] (
    [id]           INT           IDENTITY (1, 1) NOT NULL,
    [parcel_id]    BIGINT        NOT NULL,
    [courier_id]   BIGINT        NULL,
    [attempted_at] DATETIME2 (7) CONSTRAINT [df_deliveries_attempted_at] DEFAULT (sysutcdatetime()) NOT NULL,
    [outcome]      NVARCHAR (20) NULL,
    CONSTRAINT [pk_deliveries] PRIMARY KEY CLUSTERED ([id] ASC),
    FOREIGN KEY ([courier_id]) REFERENCES [dbo].[couriers] ([id]),
    CONSTRAINT [fk_deliveries_parcel] FOREIGN KEY ([parcel_id]) REFERENCES [dbo].[parcels] ([id])
);


GO

CREATE TABLE [dbo].[depots] (
    [code] VARCHAR (10)   NOT NULL,
    [name] NVARCHAR (100) NOT NULL,
    [city] NVARCHAR (100) NULL,
    CONSTRAINT [pk_depots] PRIMARY KEY CLUSTERED ([code] ASC)
);


GO

CREATE TABLE [dbo].[parcels] (
    [id]              BIGINT         IDENTITY (1, 1) NOT NULL,
    [tracking_number] VARCHAR (20)   NOT NULL,
    [depot_code]      VARCHAR (10)   NULL,
    [weight_grams]    INT            NOT NULL,
    [status]          VARCHAR (20)   NOT NULL,
    [label_note]      NVARCHAR (100) CONSTRAINT [df_parcels_label_note] DEFAULT (N'fragile -- handle with care; don''t stack') NULL,
    [sender]          NVARCHAR (100) NULL,
    [row_version]     ROWVERSION     NOT NULL,
    [weight_kg]       AS             ([weight_grams]/(1000.0)) PERSISTED,
    [order]           VARCHAR (32)   NULL,
    CONSTRAINT [pk_parcels] PRIMARY KEY CLUSTERED ([id] ASC),
    CONSTRAINT [fk_parcels_depot] FOREIGN KEY ([depot_code]) REFERENCES [dbo].[depots] ([code]),
    CONSTRAINT [uq_parcels_tracking_number] UNIQUE NONCLUSTERED ([tracking_number] ASC)
);


GO

CREATE TABLE [dbo].[couriers] (
    [id]         BIGINT         IDENTITY (1, 1) NOT NULL,
    [depot_code] VARCHAR (10)   NULL,
    [full_name]  NVARCHAR (100) NOT NULL,
    [phone]      VARCHAR (32)   NULL,
    PRIMARY KEY CLUSTERED ([id] ASC),
    FOREIGN KEY ([depot_code]) REFERENCES [dbo].[depots] ([code])
);


GO

CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

GO

CREATE NONCLUSTERED INDEX [ix_parcels_status]
    ON [dbo].[parcels]([status] ASC);


GO


CREATE FUNCTION depot_label (@code varchar(10)) RETURNS nvarchar(40) AS
BEGIN
    RETURN N'Depot ' + @code + N'; ''' + UPPER(@code) + N'''';
END;

GO


CREATE TRIGGER parcels_default_status ON parcels AFTER INSERT AS
BEGIN
    SET NOCOUNT ON;
    -- an empty status is 'registered'; the depot's scan sets the next one
    UPDATE p SET status = 'registered'
    FROM parcels p JOIN inserted i ON i.id = p.id
    WHERE i.status = '';
END;

GO

EXECUTE sp_addextendedproperty @name = N'MS_Description', @value = N'A parcel''s journey; from depot to door', @level0type = N'SCHEMA', @level0name = N'dbo', @level1type = N'TABLE', @level1name = N'parcels';


GO

