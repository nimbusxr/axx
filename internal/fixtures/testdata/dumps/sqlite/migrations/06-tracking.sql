CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

CREATE TRIGGER parcels_default_status AFTER INSERT ON parcels
WHEN NEW.status = ''
BEGIN
    -- an empty status is 'registered'; the depot's scan sets the next one
    UPDATE parcels SET status = 'registered' WHERE id = NEW.id;
END;
