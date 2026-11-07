CREATE TABLE deliveries (
    id INTEGER PRIMARY KEY,
    parcel_id INTEGER NOT NULL,
    courier_id INTEGER REFERENCES drivers (id),
    attempted_at TEXT NOT NULL,
    signature TEXT
);

ALTER TABLE deliveries ADD COLUMN depot_code TEXT REFERENCES depots (code);
