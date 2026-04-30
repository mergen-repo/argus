-- FIX-308 down: drop circuit_state column + constraint.

ALTER TABLE operators DROP CONSTRAINT IF EXISTS chk_operator_circuit_state;
ALTER TABLE operators DROP COLUMN IF EXISTS circuit_state;
