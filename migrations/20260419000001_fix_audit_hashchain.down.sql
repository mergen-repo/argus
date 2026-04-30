DROP TRIGGER IF EXISTS trg_audit_chain_guard ON audit_logs;
DROP FUNCTION IF EXISTS audit_chain_guard();
DROP INDEX IF EXISTS idx_audit_id_desc;
