-- SEED-01: Super Admin Account
-- Idempotent: uses ON CONFLICT DO NOTHING

-- Create demo tenant
INSERT INTO tenants (id, name, domain, contact_email, state)
VALUES (
    '00000000-0000-0000-0000-000000000001',
    'Argus Demo',
    'demo.argus.io',
    'admin@argus.io',
    'active'
) ON CONFLICT (domain) DO NOTHING;

-- Create super admin user
-- Password: admin (bcrypt cost 12)
INSERT INTO users (id, tenant_id, email, password_hash, name, role, state)
VALUES (
    '00000000-0000-0000-0000-000000000010',
    '00000000-0000-0000-0000-000000000001',
    'admin@argus.io',
    '$2a$12$n3ENQ9C2SubnV9Du.wAB2OagzQLtHurJ.ISjL8NJaMuiviByYFvSG',
    'System Admin',
    'super_admin',
    'active'
) ON CONFLICT DO NOTHING;
