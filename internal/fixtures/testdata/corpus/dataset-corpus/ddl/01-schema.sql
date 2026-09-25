CREATE SCHEMA IF NOT EXISTS space;

-- mission lifecycle rows
CREATE TABLE space.missions (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'planned',
    launch_date TIMESTAMP,
    destination VARCHAR(255),
    budget NUMERIC(12,2),
    metadata JSONB
);

CREATE TABLE space.spacecraft (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    capacity INTEGER,
    specs JSONB,
    CONSTRAINT spacecraft_name_unique UNIQUE (name)
);

ALTER TABLE space.missions ADD COLUMN crew_size INTEGER DEFAULT 0;
