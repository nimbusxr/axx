CREATE VIEW parcels_in_transit AS
    SELECT p.id, p.tracking_number, c.full_name AS courier
    FROM parcels p
    JOIN deliveries d ON d.parcel_id = p.id
    LEFT JOIN couriers c ON c.id = d.courier_id
    WHERE p.status = 'in_transit';

CREATE FUNCTION parcels_default_status() RETURNS trigger
LANGUAGE plpgsql AS $body$
BEGIN
    -- an empty status is 'registered'; the depot's scan sets the next one
    IF NEW.status = '' THEN
        NEW.status := 'registered';
    END IF;
    RAISE DEBUG 'parcel %: status %; label ''%''', NEW.tracking_number, NEW.status, $$as printed$$;
    RETURN NEW;
END;
$body$;

CREATE TRIGGER parcels_default_status BEFORE INSERT OR UPDATE OF status ON parcels
    FOR EACH ROW EXECUTE FUNCTION parcels_default_status();

CREATE FUNCTION depot_label(code text) RETURNS text
LANGUAGE sql IMMUTABLE AS $$ SELECT 'Depot ' || code || '; ''' || upper(code) || '''' $$;
