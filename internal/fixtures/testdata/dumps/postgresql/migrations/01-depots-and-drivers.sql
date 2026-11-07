-- Depots are keyed by their code; drivers work out of one.
CREATE TABLE depots (
    code varchar(10),
    name text NOT NULL,
    city text,
    PRIMARY KEY (code)
) WITH (fillfactor = 90);

CREATE TABLE drivers (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    depot_code varchar(10) REFERENCES depots (code),
    full_name text NOT NULL,
    phone varchar(20),
    vehicle text
);

CREATE TABLE route_plans (
    id serial PRIMARY KEY,
    depot_code varchar(10) NOT NULL REFERENCES depots (code),
    planned_for date NOT NULL
);
