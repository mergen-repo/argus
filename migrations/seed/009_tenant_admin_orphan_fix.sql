-- SEED-09: Tenant Admin Orphan Fix (FIX-246 AC-9)
--
-- For every tenant that has zero non-terminated users, insert one tenant_admin
-- user so that user_count never shows 0 on the Tenant Usage dashboard.
--
-- Idempotent: WHERE NOT EXISTS guard + ON CONFLICT DO NOTHING.
--
-- password_hash is a deliberate force-reset stub — not a valid bcrypt hash.
-- The inserted user will fail authentication until the password is reset via
-- the admin console or a password-reset flow. password_change_required=true
-- enforces this on first login.

BEGIN;

INSERT INTO users (tenant_id, email, password_hash, name, role, state, password_change_required)
SELECT
  t.id,
  'admin+' || lower(regexp_replace(t.name, '[^a-zA-Z0-9]', '', 'g')) || '@argus.local',
  '$2a$10$00000000000000000000000000000000000000000000000000000000',
  t.name || ' Admin',
  'tenant_admin',
  'active',
  true
FROM tenants t
WHERE NOT EXISTS (
  SELECT 1 FROM users u
  WHERE u.tenant_id = t.id
    AND u.state != 'terminated'
)
ON CONFLICT DO NOTHING;

COMMIT;
