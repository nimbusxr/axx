CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

CREATE OR REPLACE TRIGGER parcels_lower_status
BEFORE INSERT OR UPDATE OF status ON parcels
FOR EACH ROW
BEGIN
    -- statuses are lower case; 'IN_TRANSIT' is 'in_transit'
    :NEW.status := LOWER(:NEW.status);
END;
/

CREATE OR REPLACE FUNCTION depot_label (code IN varchar2) RETURN varchar2 DETERMINISTIC IS
BEGIN
    RETURN 'Depot ' || code || '; ''' || UPPER(code) || '''';
END;
/
