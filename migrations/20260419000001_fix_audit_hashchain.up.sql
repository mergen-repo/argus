-- FIX-104: Audit hash chain integrity
-- Adds BEFORE INSERT trigger as defense-in-depth guard for chain integrity
-- and a descending index on id for efficient tail reads.

CREATE INDEX IF NOT EXISTS idx_audit_id_desc ON audit_logs (id DESC);

CREATE OR REPLACE FUNCTION audit_chain_guard() RETURNS TRIGGER AS $$
DECLARE
  tail_hash VARCHAR(64);
BEGIN
  SELECT hash INTO tail_hash FROM audit_logs ORDER BY id DESC LIMIT 1;
  IF tail_hash IS NULL THEN
    IF NEW.prev_hash != '0000000000000000000000000000000000000000000000000000000000000000' THEN
      RAISE EXCEPTION 'audit_chain_violation: first row prev_hash must be genesis';
    END IF;
  ELSE
    IF NEW.prev_hash != tail_hash THEN
      RAISE EXCEPTION 'audit_chain_violation: prev_hash (%) does not match tail hash (%)', NEW.prev_hash, tail_hash;
    END IF;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_chain_guard
  BEFORE INSERT ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION audit_chain_guard();
