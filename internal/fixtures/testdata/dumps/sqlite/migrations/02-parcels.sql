CREATE TABLE parcels (
    id INTEGER NOT NULL, -- the parcel's own number; SQLite assigns it
    tracking_number TEXT NOT NULL UNIQUE,
    depot_code TEXT REFERENCES depots (code),
    weight_grams INTEGER,
    weight_kg REAL GENERATED ALWAYS AS (weight_grams / 1000.0) VIRTUAL,
    status TEXT NOT NULL DEFAULT 'registered',
    "order" TEXT, /* the shop's order number -- as the sender gave it */
    label_note TEXT DEFAULT 'fragile -- handle with care; don''t stack',
    sender_name TEXT,
    PRIMARY KEY (id)
);

CREATE INDEX parcels_status_idx ON parcels (status);
