-- FIX-308: persist live circuit-breaker state on operators.circuit_state.
-- Pre-fix the column was modeled on the Go Operator struct but never existed
-- in the DB — the field was always zero-valued. The CB transition handler
-- now UPDATEs this column alongside operator_health_logs (see
-- internal/operator/health.go FIX-308 hook).
--
-- Default 'closed' matches the gobreaker library's initial state so existing
-- rows boot into a sensible value without a NULL backfill.

ALTER TABLE operators
    ADD COLUMN IF NOT EXISTS circuit_state VARCHAR(20) NOT NULL DEFAULT 'closed';

-- Constrain to known CB states for read-side parity. gobreaker emits one of
-- 'closed' / 'half-open' / 'open' (we lower-snake-case 'half_open' for
-- consistency with our log-table conventions).
ALTER TABLE operators
    DROP CONSTRAINT IF EXISTS chk_operator_circuit_state;
ALTER TABLE operators
    ADD CONSTRAINT chk_operator_circuit_state
    CHECK (circuit_state IN ('closed', 'half_open', 'open'));
