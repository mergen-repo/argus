-- Trigger-based integrity check for sim_id columns in tables referencing partitioned sims table.
-- Hard FK impossible due to LIST partition + composite PK. DEV-169.

DROP TRIGGER IF EXISTS trg_esim_profiles_check_sim ON esim_profiles;
DROP TRIGGER IF EXISTS trg_ip_addresses_check_sim ON ip_addresses;
DROP TRIGGER IF EXISTS trg_ota_commands_check_sim ON ota_commands;

DROP FUNCTION IF EXISTS check_sim_exists();
