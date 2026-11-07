CREATE TABLE depots (
    code TEXT,
    name TEXT NOT NULL,
    city TEXT,
    PRIMARY KEY (code)
) WITHOUT ROWID;
CREATE TABLE "couriers" (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    depot_code TEXT REFERENCES depots (code),
    full_name TEXT NOT NULL,
    phone TEXT);
CREATE TABLE "parcels" (
    id INTEGER NOT NULL, -- the parcel's own number; SQLite assigns it
    tracking_number TEXT NOT NULL UNIQUE,
    depot_code TEXT REFERENCES depots (code),
    weight_grams INTEGER NOT NULL,
    weight_kg REAL GENERATED ALWAYS AS (weight_grams / 1000.0) VIRTUAL,
    status TEXT NOT NULL, -- given by the depot that registers it
    "order" TEXT, /* the shop's order number -- as the sender gave it */
    label_note TEXT DEFAULT 'fragile -- handle with care; don''t stack',
    sender TEXT,
    PRIMARY KEY (id)
);
CREATE TABLE "deliveries" (
    id INTEGER PRIMARY KEY,
    parcel_id INTEGER NOT NULL REFERENCES parcels (id),
    courier_id INTEGER REFERENCES "couriers" (id),
    attempted_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    depot_code TEXT REFERENCES depots (code)
, outcome TEXT) STRICT;
