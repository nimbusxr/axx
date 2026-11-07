-- Depots are keyed by their code; drivers work out of one.
CREATE TABLE depots (
    code varchar(10),
    name varchar(100) NOT NULL,
    city varchar(100),
    PRIMARY KEY (code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE drivers (
    id bigint NOT NULL AUTO_INCREMENT PRIMARY KEY,
    depot_code varchar(10),
    full_name varchar(100) NOT NULL,
    phone varchar(20),
    vehicle varchar(40),
    CONSTRAINT drivers_depot FOREIGN KEY (depot_code) REFERENCES depots (code)
);

CREATE TABLE route_plans (
    id int AUTO_INCREMENT,
    depot_code varchar(10) NOT NULL,
    planned_for date NOT NULL,
    PRIMARY KEY (id),
    FOREIGN KEY (depot_code) REFERENCES depots (code)
);
