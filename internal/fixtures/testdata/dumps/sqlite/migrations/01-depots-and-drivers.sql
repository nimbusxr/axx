-- Depots are keyed by their code; drivers work out of one.
CREATE TABLE depots (
    code TEXT,
    name TEXT NOT NULL,
    city TEXT,
    PRIMARY KEY (code)
) WITHOUT ROWID;

CREATE TABLE drivers (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    depot_code TEXT REFERENCES depots (code),
    full_name TEXT NOT NULL,
    phone TEXT,
    vehicle TEXT
);

CREATE TABLE route_plans (
    id INTEGER PRIMARY KEY,
    depot_code TEXT NOT NULL REFERENCES depots (code),
    planned_for TEXT NOT NULL
);
