-- Revert: restore 'auditor' in constraint, rename back
UPDATE users SET role = 'auditor' WHERE role = 'analyst';

ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_role;

ALTER TABLE users ADD CONSTRAINT chk_users_role CHECK (
    role IN ('super_admin', 'tenant_admin', 'sim_manager', 'policy_editor',
             'compliance_officer', 'auditor', 'api_user')
);
