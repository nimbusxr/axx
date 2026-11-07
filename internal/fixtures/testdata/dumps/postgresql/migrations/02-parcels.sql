CREATE TABLE parcels (
    id bigserial PRIMARY KEY,
    tracking_number varchar(20) NOT NULL UNIQUE,
    depot_code varchar(10) REFERENCES depots (code),
    weight_grams integer,
    weight_kg numeric GENERATED ALWAYS AS (weight_grams / 1000.0) STORED,
    status text NOT NULL DEFAULT 'registered',
    "order" varchar(32),
    label_note text DEFAULT 'fragile -- handle with care; don''t stack',
    sender_name text
);

CREATE INDEX parcels_status_idx ON parcels (status);

COMMENT ON TABLE parcels IS E'A parcel\'s journey; from depot to door';
COMMENT ON COLUMN parcels."order" IS 'The shop''s order number -- as the sender gave it';
