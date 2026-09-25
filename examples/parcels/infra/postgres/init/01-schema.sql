-- The parcels service's database. The acceptance suite's seeds must match it
-- (the `conformance` rule in ../../../acceptance/axx.yaml).
CREATE SCHEMA IF NOT EXISTS parcels;

CREATE SEQUENCE parcels.label_numbers START 100000001;

CREATE TABLE parcels.parcels (
    reference     VARCHAR(40) PRIMARY KEY,
    sender        VARCHAR(100) NOT NULL,
    status        VARCHAR(20) NOT NULL DEFAULT 'REGISTERED',
    weight_grams  INTEGER NOT NULL,
    service_level VARCHAR(20) NOT NULL DEFAULT 'STANDARD',
    recipient     JSONB NOT NULL,
    details       JSONB NOT NULL DEFAULT '{}',
    label_number  BIGINT NOT NULL DEFAULT nextval('parcels.label_numbers'),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX parcels_sender_idx ON parcels.parcels (sender, created_at);

-- Shops' systems insert manifest lines (status PENDING); the service imports
-- them and marks each line IMPORTED or REJECTED.
CREATE TABLE parcels.manifest_lines (
    id               VARCHAR(64) PRIMARY KEY,
    manifest_id      VARCHAR(64) NOT NULL,
    reference        VARCHAR(40) NOT NULL,
    sender           VARCHAR(100) NOT NULL,
    weight_grams     INTEGER NOT NULL,
    service_level    VARCHAR(20) NOT NULL DEFAULT 'STANDARD',
    recipient        JSONB NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'PENDING',
    error            TEXT,
    parcel_reference VARCHAR(40),
    received_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at     TIMESTAMPTZ
);

CREATE INDEX manifest_lines_status_idx ON parcels.manifest_lines (status, received_at);
