CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

--#SET TERMINATOR @
CREATE OR REPLACE TRIGGER parcels_lower_status
NO CASCADE BEFORE INSERT ON parcels
REFERENCING NEW AS n
FOR EACH ROW
BEGIN
    -- statuses are lower case; 'IN_TRANSIT' is 'in_transit'
    SET n.status = LOWER(n.status);
END
@

CREATE OR REPLACE FUNCTION depot_label (code varchar(10)) RETURNS varchar(40) DETERMINISTIC NO EXTERNAL ACTION
BEGIN
    RETURN 'Depot ' || code || '; ''' || UPPER(code) || '''';
END
@
--#SET TERMINATOR ;
