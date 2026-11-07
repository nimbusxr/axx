CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';
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

CREATE FUNCTION depot_label (@code varchar(10)) RETURNS nvarchar(40) AS
BEGIN
    RETURN N'Depot ' + @code + N'; ''' + UPPER(@code) + N'''';
END;
GO
