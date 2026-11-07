/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `couriers` (
  `id` bigint NOT NULL AUTO_INCREMENT,
  `full_name` varchar(100) NOT NULL,
  `phone` varchar(20) DEFAULT NULL,
  `depot_code` varchar(10) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `drivers_depot` (`depot_code`),
  CONSTRAINT `drivers_depot` FOREIGN KEY (`depot_code`) REFERENCES `depots` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `deliveries` (
  `id` int NOT NULL AUTO_INCREMENT,
  `parcel_id` bigint unsigned NOT NULL,
  `courier_id` bigint DEFAULT NULL,
  `attempted_at` datetime NOT NULL DEFAULT (now()),
  `outcome` varchar(20) NOT NULL DEFAULT 'pending',
  `proof_photo` varchar(255) DEFAULT NULL,
  PRIMARY KEY (`id`),
  KEY `courier_id` (`courier_id`),
  KEY `deliveries_parcel` (`parcel_id`),
  CONSTRAINT `deliveries_ibfk_1` FOREIGN KEY (`courier_id`) REFERENCES `couriers` (`id`),
  CONSTRAINT `deliveries_parcel` FOREIGN KEY (`parcel_id`) REFERENCES `parcels` (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `depots` (
  `region` char(2) NOT NULL,
  `code` varchar(10) NOT NULL,
  `name` varchar(100) NOT NULL,
  `city` varchar(100) DEFAULT NULL,
  PRIMARY KEY (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
/*!40101 SET character_set_client = @saved_cs_client */;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `parcels` (
  `id` bigint unsigned NOT NULL AUTO_INCREMENT,
  `tracking_number` varchar(20) NOT NULL,
  `depot_code` varchar(10) DEFAULT NULL,
  `weight_grams` int NOT NULL,
  `weight_kg` decimal(10,3) GENERATED ALWAYS AS ((`weight_grams` / 1000)) STORED,
  `status` varchar(20) NOT NULL,
  `order` varchar(32) DEFAULT NULL COMMENT 'The shop''s order number -- as the sender gave it',
  `label_note` varchar(100) DEFAULT 'fragile -- handle with care; don''t stack',
  `sender` varchar(100) DEFAULT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `parcels_tracking_number` (`tracking_number`),
  KEY `parcels_depot` (`depot_code`),
  KEY `parcels_status` (`status`),
  CONSTRAINT `parcels_depot` FOREIGN KEY (`depot_code`) REFERENCES `depots` (`code`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='A parcel''s journey; from depot to door';
/*!40101 SET character_set_client = @saved_cs_client */;
/*!50003 SET @saved_cs_client      = @@character_set_client */ ;
/*!50003 SET @saved_cs_results     = @@character_set_results */ ;
/*!50003 SET @saved_col_connection = @@collation_connection */ ;
/*!50003 SET character_set_client  = latin1 */ ;
/*!50003 SET character_set_results = latin1 */ ;
/*!50003 SET collation_connection  = latin1_swedish_ci */ ;
/*!50003 SET @saved_sql_mode       = @@sql_mode */ ;
/*!50003 SET sql_mode              = 'ONLY_FULL_GROUP_BY,STRICT_TRANS_TABLES,NO_ZERO_IN_DATE,NO_ZERO_DATE,ERROR_FOR_DIVISION_BY_ZERO,NO_ENGINE_SUBSTITUTION' */ ;
DELIMITER ;;
/*!50003 CREATE*/ /*!50017 DEFINER=`root`@`localhost`*/ /*!50003 TRIGGER `parcels_default_status` BEFORE INSERT ON `parcels` FOR EACH ROW BEGIN
    -- an empty status is 'registered'; the depot's scan sets the next one
    IF NEW.status = '' THEN
        SET NEW.status = 'registered';
    END IF;
END */;;
DELIMITER ;
/*!50003 SET sql_mode              = @saved_sql_mode */ ;
/*!50003 SET character_set_client  = @saved_cs_client */ ;
/*!50003 SET character_set_results = @saved_cs_results */ ;
/*!50003 SET collation_connection  = @saved_col_connection */ ;
SET @saved_cs_client     = @@character_set_client;
/*!50503 SET character_set_client = utf8mb4 */;
/*!50001 CREATE VIEW `parcels_in_transit` AS SELECT 
 1 AS `id`,
 1 AS `tracking_number`,
 1 AS `courier`*/;
SET character_set_client = @saved_cs_client;
/*!50001 DROP VIEW IF EXISTS `parcels_in_transit`*/;
/*!50001 SET @saved_cs_client          = @@character_set_client */;
/*!50001 SET @saved_cs_results         = @@character_set_results */;
/*!50001 SET @saved_col_connection     = @@collation_connection */;
/*!50001 SET character_set_client      = latin1 */;
/*!50001 SET character_set_results     = latin1 */;
/*!50001 SET collation_connection      = latin1_swedish_ci */;
/*!50001 CREATE ALGORITHM=UNDEFINED */
/*!50013 DEFINER=`root`@`localhost` SQL SECURITY DEFINER */
/*!50001 VIEW `parcels_in_transit` AS select `p`.`id` AS `id`,`p`.`tracking_number` AS `tracking_number`,`c`.`full_name` AS `courier` from ((`parcels` `p` join `deliveries` `d` on((`d`.`parcel_id` = `p`.`id`))) left join `couriers` `c` on((`c`.`id` = `d`.`courier_id`))) where (`p`.`status` = 'in_transit') */;
/*!50001 SET character_set_client      = @saved_cs_client */;
/*!50001 SET character_set_results     = @saved_cs_results */;
/*!50001 SET collation_connection      = @saved_col_connection */;
