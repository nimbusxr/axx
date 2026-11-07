-- Fill in the weights the depots scanned, then require one. SQLite cannot
-- change a column's constraints in place: the tables are built again.
CREATE TEMP TABLE scanned_weights (
    tracking_number TEXT NOT NULL,
    weight_grams INTEGER NOT NULL
);

CREATE TABLE parcels_new (
    id INTEGER NOT NULL, -- the parcel's own number; SQLite assigns it
    tracking_number TEXT NOT NULL UNIQUE,
    depot_code TEXT REFERENCES depots (code),
    weight_grams INTEGER NOT NULL,
    weight_kg REAL GENERATED ALWAYS AS (weight_grams / 1000.0) VIRTUAL,
    status TEXT NOT NULL, -- given by the depot that registers it
    "order" TEXT, /* the shop's order number -- as the sender gave it */
    label_note TEXT DEFAULT 'fragile -- handle with care; don''t stack',
    sender_name TEXT,
    PRIMARY KEY (id)
);
INSERT INTO parcels_new (id, tracking_number, depot_code, weight_grams, status, "order", label_note, sender_name)
SELECT p.id, p.tracking_number, p.depot_code, coalesce(s.weight_grams, p.weight_grams), p.status, p."order", p.label_note, p.sender_name
FROM parcels p LEFT JOIN scanned_weights s ON s.tracking_number = p.tracking_number;
DROP TABLE parcels;
ALTER TABLE parcels_new RENAME TO parcels;
CREATE INDEX parcels_status_idx ON parcels (status);

CREATE TABLE deliveries_new (
    id INTEGER PRIMARY KEY,
    parcel_id INTEGER NOT NULL REFERENCES parcels (id),
    courier_id INTEGER REFERENCES drivers (id),
    attempted_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    signature TEXT,
    depot_code TEXT REFERENCES depots (code)
) STRICT;
INSERT INTO deliveries_new SELECT * FROM deliveries;
DROP TABLE deliveries;
ALTER TABLE deliveries_new RENAME TO deliveries;
