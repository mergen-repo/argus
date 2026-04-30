-- FIX-110: Align DB role CHECK constraint with RBAC roleLevels map
-- roleLevels uses 'analyst' but DB constraint only has 'auditor'

UPDATE users SET role = 'analyst' WHERE role = 'auditor';

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_role;

ALTER TABLE users ADD CONSTRAINT chk_users_role CHECK (
    role IN ('super_admin', 'tenant_admin', 'sim_manager', 'policy_editor',
             'operator_manager', 'compliance_officer', 'analyst', 'api_user')
);
