-- Trigger-based integrity check for sim_id columns in tables referencing partitioned sims table.
-- Hard FK impossible due to LIST partition + composite PK. DEV-169.

CREATE OR REPLACE FUNCTION check_sim_exists()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.sim_id IS NULL THEN
        RETURN NEW;  -- nullable sim_id (e.g. ip_addresses) OK
    END IF;
    IF NOT EXISTS (SELECT 1 FROM sims WHERE id = NEW.sim_id) THEN
        RAISE EXCEPTION 'FK violation: sim_id % does not exist in sims', NEW.sim_id
            USING ERRCODE = 'foreign_key_violation';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_esim_profiles_check_sim
    BEFORE INSERT OR UPDATE OF sim_id ON esim_profiles
    FOR EACH ROW EXECUTE FUNCTION check_sim_exists();

CREATE TRIGGER trg_ip_addresses_check_sim
    BEFORE INSERT OR UPDATE OF sim_id ON ip_addresses
    FOR EACH ROW EXECUTE FUNCTION check_sim_exists();

CREATE TRIGGER trg_ota_commands_check_sim
    BEFORE INSERT OR UPDATE OF sim_id ON ota_commands
    FOR EACH ROW EXECUTE FUNCTION check_sim_exists();
