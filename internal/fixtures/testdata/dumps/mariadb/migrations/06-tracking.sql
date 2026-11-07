CREATE INDEX parcels_status ON parcels (status);

CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

DELIMITER $$
CREATE TRIGGER parcels_default_status BEFORE INSERT ON parcels FOR EACH ROW
BEGIN
    -- an empty status is 'registered'; the depot's scan sets the next one
    IF NEW.status = '' THEN
        SET NEW.status = 'registered';
    END IF;
END$$

CREATE FUNCTION depot_label(code varchar(10)) RETURNS varchar(40) DETERMINISTIC
RETURN CONCAT('Depot ', code, '; ''', UPPER(code), '''')$$
DELIMITER ;
