-- Depots are keyed by their code; drivers work out of one.
CREATE TABLE depots (
    code varchar(10),
    name nvarchar(100) NOT NULL,
    city nvarchar(100) NULL,
    CONSTRAINT pk_depots PRIMARY KEY (code)
);

CREATE TABLE drivers (
    id bigint IDENTITY(1, 1) NOT NULL PRIMARY KEY,
    depot_code varchar(10) REFERENCES depots (code),
    full_name nvarchar(100) NOT NULL,
    phone varchar(20),
    vehicle nvarchar(40)
);

CREATE TABLE route_plans (
    id int IDENTITY PRIMARY KEY,
    depot_code varchar(10) NOT NULL FOREIGN KEY REFERENCES depots (code),
    planned_for date NOT NULL
);
GO
