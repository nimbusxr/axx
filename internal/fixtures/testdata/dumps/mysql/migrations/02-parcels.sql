CREATE TABLE `parcels` (
    `id` bigint unsigned NOT NULL AUTO_INCREMENT,
    `tracking_number` varchar(20) NOT NULL,
    depot_code varchar(10),
    weight_grams int,
    weight_kg decimal(10,3) GENERATED ALWAYS AS (weight_grams / 1000) STORED,
    status varchar(20) NOT NULL DEFAULT 'registered',
    `order` varchar(32) COMMENT 'The shop''s order number -- as the sender gave it',
    label_note varchar(100) DEFAULT 'fragile -- handle with care; don\'t stack',
    sender_name varchar(100),
    PRIMARY KEY (`id`),
    UNIQUE KEY parcels_tracking_number (`tracking_number`),
    CONSTRAINT parcels_depot FOREIGN KEY (depot_code) REFERENCES depots (code)
) ENGINE=InnoDB COMMENT='A parcel''s journey; from depot to door';

CREATE INDEX parcels_status ON parcels (status);
