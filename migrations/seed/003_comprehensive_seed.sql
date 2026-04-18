-- SEED-03: Comprehensive E2E Seed Data
-- Realistic Turkish data for all screens
-- Idempotent: uses ON CONFLICT DO NOTHING / DO UPDATE
-- Run after 001_admin_user.sql and 002_system_data.sql
--
-- D-015 (STORY-079): Fresh-volume fix — CHECK constraint violations, NOT RLS
-- Root cause: migration 20260412000003_enum_check_constraints added chk_* constraints
-- AFTER this seed was written. Live DB had 0 bad rows; fresh volume aborts on insert.
-- Fixes applied:
--   users.role: 'analyst' → 'auditor', 'op_manager' → 'sim_manager'
--   policy_versions.state: 'rolled_back' → 'superseded'
-- RLS is NOT the issue — argus role has rolbypassrls=t (verified).

BEGIN;

-- ============================================================
-- CLEANUP: Remove existing test data so seed is idempotent
-- ============================================================
DELETE FROM policy_assignments WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM ota_commands WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM anomalies WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM cdrs WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM sessions WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM sim_state_history WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM esim_profiles WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM ip_addresses WHERE pool_id IN (SELECT ip.id FROM ip_pools ip WHERE ip.tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM ip_pools WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM sims WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM policy_rollouts WHERE policy_version_id IN (SELECT pv.id FROM policy_versions pv JOIN policies p ON pv.policy_id = p.id WHERE p.tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM policy_assignments WHERE policy_version_id IN (SELECT pv.id FROM policy_versions pv JOIN policies p ON pv.policy_id = p.id WHERE p.tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
UPDATE policies SET current_version_id = NULL WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM policy_versions WHERE policy_id IN (SELECT id FROM policies WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'));
DELETE FROM apns WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM policies WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM jobs WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM notifications WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM api_keys WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM sim_segments WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM msisdn_pool WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM notification_configs WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM operator_grants WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DELETE FROM users WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename='tenant_retention_config') THEN DELETE FROM tenant_retention_config WHERE tenant_id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001'); END IF; END $$;
DELETE FROM tenants WHERE id NOT IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000001');

-- Clean data for our target tenants too (re-seed from scratch)
DELETE FROM policy_assignments WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM ota_commands WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM anomalies WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM cdrs WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM sessions WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM sim_state_history WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM esim_profiles WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM ip_addresses WHERE pool_id IN (SELECT ip.id FROM ip_pools ip WHERE ip.tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM ip_pools WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM sims WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM policy_rollouts WHERE policy_version_id IN (SELECT pv.id FROM policy_versions pv JOIN policies p ON pv.policy_id = p.id WHERE p.tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
UPDATE policies SET current_version_id = NULL WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM policy_versions WHERE policy_id IN (SELECT id FROM policies WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM apns WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM policies WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM jobs WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM notifications WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM api_keys WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM sim_segments WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM msisdn_pool WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM notification_configs WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM audit_logs WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM operator_grants WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DELETE FROM user_sessions WHERE user_id IN (SELECT id FROM users WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'));
DELETE FROM users WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002');
DO $$ BEGIN IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename='tenant_retention_config') THEN DELETE FROM tenant_retention_config WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001','10000000-0000-0000-0000-000000000002'); END IF; END $$;

-- Clean Argus Demo tenant data (full re-seed)
DELETE FROM policy_assignments WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = '00000000-0000-0000-0000-000000000001');
DELETE FROM anomalies WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM ota_commands WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM cdrs WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM sessions WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM sim_state_history WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = '00000000-0000-0000-0000-000000000001');
DELETE FROM esim_profiles WHERE sim_id IN (SELECT id FROM sims WHERE tenant_id = '00000000-0000-0000-0000-000000000001');
DELETE FROM ip_addresses WHERE pool_id IN (SELECT ip.id FROM ip_pools ip WHERE ip.tenant_id = '00000000-0000-0000-0000-000000000001');
DELETE FROM ip_pools WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM sims WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM policy_rollouts WHERE policy_version_id IN (SELECT pv.id FROM policy_versions pv JOIN policies p ON pv.policy_id = p.id WHERE p.tenant_id = '00000000-0000-0000-0000-000000000001');
UPDATE policies SET current_version_id = NULL WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM policy_versions WHERE policy_id IN (SELECT id FROM policies WHERE tenant_id = '00000000-0000-0000-0000-000000000001');
DELETE FROM apns WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM policies WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM jobs WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM notifications WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM api_keys WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM sim_segments WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM msisdn_pool WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM notification_configs WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM audit_logs WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM operator_grants WHERE tenant_id = '00000000-0000-0000-0000-000000000001';
DELETE FROM operator_grants WHERE operator_id = 'bd2508e7-85c3-4c31-b07b-f12fb0beebef';
DELETE FROM user_sessions WHERE user_id IN (SELECT id FROM users WHERE tenant_id = '00000000-0000-0000-0000-000000000001' AND email != 'admin@argus.io');
DELETE FROM users WHERE tenant_id = '00000000-0000-0000-0000-000000000001' AND email != 'admin@argus.io';

-- Remove old test operator that conflicts with Turkcell mcc/mnc
DELETE FROM operator_health_logs WHERE operator_id = 'bd2508e7-85c3-4c31-b07b-f12fb0beebef';
DELETE FROM operators WHERE id = 'bd2508e7-85c3-4c31-b07b-f12fb0beebef' AND code = 'TOPG';

-- ============================================================
-- TENANTS (2 tenants + existing Argus Demo)
-- ============================================================
INSERT INTO tenants (id, name, domain, contact_email, contact_phone, max_sims, max_apns, max_users, state) VALUES
('10000000-0000-0000-0000-000000000001', 'Nar Teknoloji', 'nar.com.tr', 'info@nar.com.tr', '+902125551234', 500000, 200, 100, 'active'),
('10000000-0000-0000-0000-000000000002', 'Bosphorus IoT', 'bosphorus-iot.com', 'admin@bosphorus-iot.com', '+902163339876', 200000, 100, 50, 'active')
ON CONFLICT (domain) DO NOTHING;

-- ============================================================
-- OPERATORS (3 Turkish operators)
-- ============================================================
-- STORY-090 Wave 2 D2-B: adapter_type column removed; every operator's
-- adapter_config carries the nested protocol enablement flags.
--
-- STORY-090 Gate (F-A6): each enabled RADIUS operator carries its
-- canonical `radius` sub-key (shared_secret, listen_addr, host, port)
-- so the adapter factory can consume the config directly. Mock
-- sibling is retained with enabled=true so the simulator keeps its
-- RADIUS-style secret lookup path while no real network handshake is
-- required in the dev environment.
INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, sm_dp_plus_url, supported_rat_types, health_status, failover_policy, state) VALUES
('20000000-0000-0000-0000-000000000001', 'Turkcell', 'turkcell', '286', '01',
 '{"radius":{"enabled":true,"shared_secret":"tc-secret","listen_addr":":1812","host":"radius.turkcell.com.tr","port":1812,"timeout_ms":3000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}',
 'https://smdp.turkcell.com.tr/api/v1',
 ARRAY['nb_iot','lte_m','lte','nr_5g'], 'healthy', 'fallback', 'active'),
('20000000-0000-0000-0000-000000000002', 'Vodafone TR', 'vodafone_tr', '286', '02',
 '{"radius":{"enabled":true,"shared_secret":"vf-secret","listen_addr":":1812","host":"radius.vodafone.com.tr","port":1812,"timeout_ms":3000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}',
 'https://smdp.vodafone.com.tr/api/v1',
 ARRAY['nb_iot','lte_m','lte','nr_5g'], 'healthy', 'reject', 'active'),
('20000000-0000-0000-0000-000000000003', 'Turk Telekom', 'turk_telekom', '286', '03',
 '{"radius":{"enabled":true,"shared_secret":"tt-secret","listen_addr":":1812","host":"radius.turktelekom.com.tr","port":1812,"timeout_ms":3000},"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":500}}',
 'https://smdp.turktelekom.com.tr/api/v1',
 ARRAY['lte','nr_5g'], 'degraded', 'queue', 'active')
ON CONFLICT (code) DO NOTHING;

-- Create SIM partitions for operators
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_turkcell') THEN
        EXECUTE format('CREATE TABLE sims_turkcell PARTITION OF sims FOR VALUES IN (%L)', '20000000-0000-0000-0000-000000000001');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_vodafone') THEN
        EXECUTE format('CREATE TABLE sims_vodafone PARTITION OF sims FOR VALUES IN (%L)', '20000000-0000-0000-0000-000000000002');
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_class WHERE relname = 'sims_turk_telekom') THEN
        EXECUTE format('CREATE TABLE sims_turk_telekom PARTITION OF sims FOR VALUES IN (%L)', '20000000-0000-0000-0000-000000000003');
    END IF;
END $$;

-- ============================================================
-- OPERATOR GRANTS (both tenants get all 3 operators)
-- ============================================================
INSERT INTO operator_grants (id, tenant_id, operator_id, enabled) VALUES
('30000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', true),
('30000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', true),
('30000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', true),
('30000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', true),
('30000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', true)
ON CONFLICT DO NOTHING;

-- ============================================================
-- USERS (5+ per tenant with different roles)
-- Password for all: "password123" bcrypt cost 12
-- ============================================================
INSERT INTO users (id, tenant_id, email, password_hash, name, role, state, totp_enabled, last_login_at) VALUES
-- Nar Teknoloji users
('40000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'ahmet.yilmaz@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Ahmet Yilmaz', 'tenant_admin', 'active', false, NOW() - INTERVAL '2 hours'),
('40000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'elif.kaya@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Elif Kaya', 'sim_manager', 'active', false, NOW() - INTERVAL '30 minutes'),
('40000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'mehmet.demir@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Mehmet Demir', 'policy_editor', 'active', true, NOW() - INTERVAL '1 day'),
('40000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'zeynep.ozturk@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Zeynep Ozturk', 'auditor', 'active', false, NOW() - INTERVAL '4 hours'),
('40000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', 'can.aksoy@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Can Aksoy', 'sim_manager', 'active', false, NOW() - INTERVAL '6 hours'),
('40000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001', 'selin.celik@nar.com.tr',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Selin Celik', 'api_user', 'active', false, NULL),
-- Bosphorus IoT users
('40000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', 'baris.topcu@bosphorus-iot.com',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Baris Topcu', 'tenant_admin', 'active', true, NOW() - INTERVAL '1 hour'),
('40000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', 'ayse.sahin@bosphorus-iot.com',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Ayse Sahin', 'sim_manager', 'active', false, NOW() - INTERVAL '3 hours'),
('40000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', 'emre.karaca@bosphorus-iot.com',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Emre Karaca', 'policy_editor', 'active', false, NOW() - INTERVAL '5 hours'),
('40000000-0000-0000-0000-000000000014', '10000000-0000-0000-0000-000000000002', 'deniz.arslan@bosphorus-iot.com',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Deniz Arslan', 'auditor', 'active', false, NOW() - INTERVAL '12 hours'),
('40000000-0000-0000-0000-000000000015', '10000000-0000-0000-0000-000000000002', 'hakan.yildiz@bosphorus-iot.com',
 '$2b$12$ZBpIqGQSR1kUn5Dl4dnbOuyAi5sH2J4tPa6a8YXphpyAhWqSnRb9W', 'Hakan Yildiz', 'sim_manager', 'active', false, NOW() - INTERVAL '8 hours')
ON CONFLICT DO NOTHING;

-- ============================================================
-- POLICIES (before APNs due to default_policy_id FK)
-- ============================================================
INSERT INTO policies (id, tenant_id, name, description, scope, state, created_by) VALUES
-- Nar Teknoloji policies
('50000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'Fleet Standart QoS', 'Filo yonetimi standart hiz politikasi', 'apn', 'active', '40000000-0000-0000-0000-000000000001'),
('50000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'Sayac Dusuk Bant', 'Akilli sayac dusuk bant genisligi politikasi', 'apn', 'active', '40000000-0000-0000-0000-000000000003'),
('50000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'Premium IoT', 'Premium IoT cihazlar icin yuksek oncelikli QoS', 'global', 'active', '40000000-0000-0000-0000-000000000003'),
('50000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'NB-IoT Tasarruf', 'NB-IoT cihazlar icin enerji tasarruflu politika', 'operator', 'active', '40000000-0000-0000-0000-000000000003'),
('50000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', 'Test Politikasi', 'Deneme amacli gecici politika', 'global', 'active', '40000000-0000-0000-0000-000000000003'),
-- Bosphorus IoT policies
('50000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', 'Akilli Sehir QoS', 'Akilli sehir altyapisi icin QoS', 'apn', 'active', '40000000-0000-0000-0000-000000000011'),
('50000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', 'Tarim IoT', 'Tarimsal IoT sensörler icin politika', 'apn', 'active', '40000000-0000-0000-0000-000000000013'),
('50000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', 'Endüstriyel M2M', 'Endustriyel M2M iletisim politikasi', 'global', 'active', '40000000-0000-0000-0000-000000000013')
ON CONFLICT DO NOTHING;

-- ============================================================
-- POLICY VERSIONS (multiple versions per policy, different states)
-- ============================================================
INSERT INTO policy_versions (id, policy_id, version, dsl_content, compiled_rules, state, affected_sim_count, activated_at, created_by) VALUES
-- Fleet Standart QoS - v1 active
('51000000-0000-0000-0000-000000000001', '50000000-0000-0000-0000-000000000001', 1,
 'POLICY "fleet-std-v1" { MATCH { apn = "iot.fleet" } RULES { bandwidth_down = 5mbps; bandwidth_up = 2mbps; priority = 5 } }',
 '{"name":"fleet-std-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"iot.fleet"}]},"rules":{"defaults":{"bandwidth_down":5000000,"bandwidth_up":2000000,"priority":5},"when_blocks":[]}}',
 'active', 45, NOW() - INTERVAL '20 days', '40000000-0000-0000-0000-000000000003'),
-- Fleet Standart QoS - v2 rolling_out
('51000000-0000-0000-0000-000000000002', '50000000-0000-0000-0000-000000000001', 2,
 'POLICY "fleet-std-v2" { MATCH { apn = "iot.fleet" } RULES { bandwidth_down = 10mbps; bandwidth_up = 5mbps; priority = 4 } }',
 '{"name":"fleet-std-v2","match":{"conditions":[{"field":"apn","op":"eq","value":"iot.fleet"}]},"rules":{"defaults":{"bandwidth_down":10000000,"bandwidth_up":5000000,"priority":4},"when_blocks":[]}}',
 'rolling_out', 45, NULL, '40000000-0000-0000-0000-000000000003'),
-- Sayac Dusuk Bant - v1 active
('51000000-0000-0000-0000-000000000003', '50000000-0000-0000-0000-000000000002', 1,
 'POLICY "meter-low-v1" { MATCH { apn = "m2m.meter" } RULES { bandwidth_down = 256kbps; bandwidth_up = 128kbps; max_daily_mb = 50 } }',
 '{"name":"meter-low-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"m2m.meter"}]},"rules":{"defaults":{"bandwidth_down":256000,"bandwidth_up":128000,"max_daily_mb":50},"when_blocks":[]}}',
 'active', 30, NOW() - INTERVAL '15 days', '40000000-0000-0000-0000-000000000003'),
-- Premium IoT - v1 rolled_back, v2 active
('51000000-0000-0000-0000-000000000004', '50000000-0000-0000-0000-000000000003', 1,
 'POLICY "premium-v1" { MATCH { rat_type = "nr_5g" } RULES { bandwidth_down = 100mbps; priority = 1 } }',
 '{"name":"premium-v1","match":{"conditions":[{"field":"rat_type","op":"eq","value":"nr_5g"}]},"rules":{"defaults":{"bandwidth_down":100000000,"priority":1},"when_blocks":[]}}',
 'superseded', 10, NOW() - INTERVAL '25 days', '40000000-0000-0000-0000-000000000003'),
('51000000-0000-0000-0000-000000000005', '50000000-0000-0000-0000-000000000003', 2,
 'POLICY "premium-v2" { MATCH { rat_type = "nr_5g" OR rat_type = "lte" } RULES { bandwidth_down = 50mbps; bandwidth_up = 20mbps; priority = 2 } }',
 '{"name":"premium-v2","match":{"conditions":[{"field":"rat_type","op":"in","value":["nr_5g","lte"]}]},"rules":{"defaults":{"bandwidth_down":50000000,"bandwidth_up":20000000,"priority":2},"when_blocks":[]}}',
 'active', 15, NOW() - INTERVAL '10 days', '40000000-0000-0000-0000-000000000003'),
-- NB-IoT Tasarruf - v1 active
('51000000-0000-0000-0000-000000000006', '50000000-0000-0000-0000-000000000004', 1,
 'POLICY "nbiot-save-v1" { MATCH { rat_type = "nb_iot" } RULES { bandwidth_down = 64kbps; bandwidth_up = 32kbps; session_timeout = 3600 } }',
 '{"name":"nbiot-save-v1","match":{"conditions":[{"field":"rat_type","op":"eq","value":"nb_iot"}]},"rules":{"defaults":{"bandwidth_down":64000,"bandwidth_up":32000,"session_timeout":3600},"when_blocks":[]}}',
 'active', 20, NOW() - INTERVAL '12 days', '40000000-0000-0000-0000-000000000003'),
-- Test Politikasi - v1 draft
('51000000-0000-0000-0000-000000000007', '50000000-0000-0000-0000-000000000005', 1,
 'POLICY "test-draft" { MATCH { apn = "*" } RULES { bandwidth_down = 1mbps } }',
 '{"name":"test-draft","match":{"conditions":[{"field":"apn","op":"eq","value":"*"}]},"rules":{"defaults":{"bandwidth_down":1000000},"when_blocks":[]}}',
 'draft', NULL, NULL, '40000000-0000-0000-0000-000000000003'),
-- Bosphorus IoT policy versions
('51000000-0000-0000-0000-000000000011', '50000000-0000-0000-0000-000000000011', 1,
 'POLICY "smart-city-v1" { MATCH { apn = "iot.city" } RULES { bandwidth_down = 20mbps; bandwidth_up = 10mbps; priority = 3 } }',
 '{"name":"smart-city-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"iot.city"}]},"rules":{"defaults":{"bandwidth_down":20000000,"bandwidth_up":10000000,"priority":3},"when_blocks":[]}}',
 'active', 25, NOW() - INTERVAL '18 days', '40000000-0000-0000-0000-000000000013'),
('51000000-0000-0000-0000-000000000012', '50000000-0000-0000-0000-000000000012', 1,
 'POLICY "agri-iot-v1" { MATCH { apn = "m2m.agri" } RULES { bandwidth_down = 128kbps; bandwidth_up = 64kbps; max_daily_mb = 20 } }',
 '{"name":"agri-iot-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"m2m.agri"}]},"rules":{"defaults":{"bandwidth_down":128000,"bandwidth_up":64000,"max_daily_mb":20},"when_blocks":[]}}',
 'active', 15, NOW() - INTERVAL '14 days', '40000000-0000-0000-0000-000000000013'),
('51000000-0000-0000-0000-000000000013', '50000000-0000-0000-0000-000000000013', 1,
 'POLICY "industrial-m2m-v1" { MATCH { rat_type = "lte" } RULES { bandwidth_down = 30mbps; priority = 2; max_sessions = 3 } }',
 '{"name":"industrial-m2m-v1","match":{"conditions":[{"field":"rat_type","op":"eq","value":"lte"}]},"rules":{"defaults":{"bandwidth_down":30000000,"priority":2,"max_sessions":3},"when_blocks":[]}}',
 'active', 20, NOW() - INTERVAL '10 days', '40000000-0000-0000-0000-000000000013')
ON CONFLICT DO NOTHING;

-- Update policies.current_version_id
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000001' WHERE id = '50000000-0000-0000-0000-000000000001';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000003' WHERE id = '50000000-0000-0000-0000-000000000002';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000005' WHERE id = '50000000-0000-0000-0000-000000000003';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000006' WHERE id = '50000000-0000-0000-0000-000000000004';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000007' WHERE id = '50000000-0000-0000-0000-000000000005';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000011' WHERE id = '50000000-0000-0000-0000-000000000011';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000012' WHERE id = '50000000-0000-0000-0000-000000000012';
UPDATE policies SET current_version_id = '51000000-0000-0000-0000-000000000013' WHERE id = '50000000-0000-0000-0000-000000000013';

-- ============================================================
-- APNs (5+ per tenant)
-- ============================================================
INSERT INTO apns (id, tenant_id, operator_id, name, display_name, apn_type, supported_rat_types, default_policy_id, state, created_by) VALUES
-- Nar Teknoloji APNs
('60000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'iot.fleet', 'Filo Yonetimi', 'iot', ARRAY['lte','lte_m','nr_5g'], '50000000-0000-0000-0000-000000000001', 'active', '40000000-0000-0000-0000-000000000001'),
('60000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'm2m.meter', 'Akilli Sayac', 'm2m', ARRAY['nb_iot','lte_m'], '50000000-0000-0000-0000-000000000002', 'active', '40000000-0000-0000-0000-000000000001'),
('60000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'data.sensor', 'Sensor Verisi', 'iot', ARRAY['nb_iot','lte_m','lte'], NULL, 'active', '40000000-0000-0000-0000-000000000001'),
('60000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'iot.camera', 'IP Kamera', 'iot', ARRAY['lte','nr_5g'], '50000000-0000-0000-0000-000000000003', 'active', '40000000-0000-0000-0000-000000000001'),
('60000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', 'm2m.industrial', 'Endustriyel M2M', 'm2m', ARRAY['lte','nr_5g'], NULL, 'active', '40000000-0000-0000-0000-000000000001'),
('60000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'legacy.gprs', 'Eski GPRS', 'internet', ARRAY['lte'], NULL, 'archived', '40000000-0000-0000-0000-000000000001'),
-- Bosphorus IoT APNs
('60000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', 'iot.city', 'Akilli Sehir', 'iot', ARRAY['lte','lte_m','nr_5g'], '50000000-0000-0000-0000-000000000011', 'active', '40000000-0000-0000-0000-000000000011'),
('60000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', 'm2m.agri', 'Tarim Sensoru', 'm2m', ARRAY['nb_iot','lte_m'], '50000000-0000-0000-0000-000000000012', 'active', '40000000-0000-0000-0000-000000000011'),
('60000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', 'iot.transport', 'Ulasim IoT', 'iot', ARRAY['lte','nr_5g'], NULL, 'active', '40000000-0000-0000-0000-000000000011'),
('60000000-0000-0000-0000-000000000014', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', 'data.energy', 'Enerji Izleme', 'iot', ARRAY['nb_iot','lte_m','lte'], '50000000-0000-0000-0000-000000000013', 'active', '40000000-0000-0000-0000-000000000011'),
('60000000-0000-0000-0000-000000000015', '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', 'm2m.water', 'Su Sayaci', 'm2m', ARRAY['nb_iot'], NULL, 'active', '40000000-0000-0000-0000-000000000011')
ON CONFLICT DO NOTHING;

-- ============================================================
-- IP POOLS (per APN, some near capacity)
-- ============================================================
INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, cidr_v6, total_addresses, used_addresses, alert_threshold_warning, alert_threshold_critical, state) VALUES
-- Nar Teknoloji pools
('70000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', 'Fleet IPv4 Pool', '10.1.0.0/22', 'fd01::/64', 1022, 456, 80, 90, 'active'),
('70000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', 'Meter IPv4 Pool', '10.2.0.0/24', NULL, 254, 218, 80, 90, 'active'),  -- 85% full
('70000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000003', 'Sensor IPv4 Pool', '10.3.0.0/23', 'fd03::/64', 510, 120, 80, 90, 'active'),
('70000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000004', 'Camera IPv4 Pool', '10.4.0.0/25', NULL, 126, 115, 80, 90, 'active'),  -- 91% full - critical
('70000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000005', 'Industrial Pool', '10.5.0.0/24', 'fd05::/64', 254, 80, 80, 90, 'active'),
-- Bosphorus IoT pools
('70000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000011', 'City IoT Pool', '10.10.0.0/22', 'fd10::/64', 1022, 340, 80, 90, 'active'),
('70000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000012', 'Agri Pool', '10.11.0.0/24', NULL, 254, 45, 80, 90, 'active'),
('70000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', 'Transport Pool', '10.12.0.0/24', NULL, 254, 180, 80, 90, 'active'),
('70000000-0000-0000-0000-000000000014', '10000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000014', 'Energy Pool', '10.13.0.0/25', 'fd13::/64', 126, 112, 80, 90, 'active')  -- 89% full - warning
ON CONFLICT DO NOTHING;

-- ============================================================
-- SIMs (120 total: 80 Nar + 40 Bosphorus, varied states)
-- ============================================================

-- Helper: generate SIMs for Nar Teknoloji - Turkcell (50 SIMs)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at) VALUES
-- Active SIMs on iot.fleet (Turkcell)
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000101', '286010000000101', '905301000101', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '60 days', NOW() - INTERVAL '65 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000102', '286010000000102', '905301000102', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '55 days', NOW() - INTERVAL '60 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000103', '286010000000103', '905301000103', 'physical', 'active', 'nr_5g', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000104', '286010000000104', '905301000104', 'esim', 'active', 'lte_m', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000105', '286010000000105', '905301000105', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000106', '286010000000106', '905301000106', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000107', '286010000000107', '905301000107', 'physical', 'active', 'nr_5g', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000108', '286010000000108', '905301000108', 'esim', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000109', '286010000000109', '905301000109', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000110', '286010000000110', '905301000110', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '15 days', NOW() - INTERVAL '20 days'),
-- Active SIMs on m2m.meter (Turkcell)
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000201', '286010000000201', '905301000201', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000202', '286010000000202', '905301000202', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '48 days', NOW() - INTERVAL '53 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000203', '286010000000203', '905301000203', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000204', '286010000000204', '905301000204', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000006', NOW() - INTERVAL '42 days', NOW() - INTERVAL '47 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000205', '286010000000205', '905301000205', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000006', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000206', '286010000000206', '905301000206', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '38 days', NOW() - INTERVAL '43 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000207', '286010000000207', '905301000207', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000208', '286010000000208', '905301000208', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
-- Suspended SIMs
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000301', '286010000000301', '905301000301', 'physical', 'suspended', 'lte', '51000000-0000-0000-0000-000000000001', NOW() - INTERVAL '90 days', NOW() - INTERVAL '95 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000302', '286010000000302', '905301000302', 'physical', 'suspended', 'nb_iot', '51000000-0000-0000-0000-000000000003', NOW() - INTERVAL '80 days', NOW() - INTERVAL '85 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000303', '286010000000303', '905301000303', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '70 days', NOW() - INTERVAL '75 days'),
-- Ordered SIMs
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000401', '286010000000401', '905301000401', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '5 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000402', '286010000000402', '905301000402', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '3 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', NULL, '89900100000000000403', '286010000000403', '905301000403', 'esim', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '1 day'),
-- Terminated SIMs
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000501', '286010000000501', '905301000501', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '120 days', NOW() - INTERVAL '125 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000002', '89900100000000000502', '286010000000502', '905301000502', 'physical', 'terminated', 'nb_iot', NULL, NOW() - INTERVAL '100 days', NOW() - INTERVAL '110 days'),
-- Stolen/lost SIM
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000001', '89900100000000000601', '286010000000601', '905301000601', 'physical', 'stolen_lost', 'lte', NULL, NOW() - INTERVAL '30 days', NOW() - INTERVAL '60 days')
ON CONFLICT DO NOTHING;

-- Nar Teknoloji - Vodafone SIMs (20)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at) VALUES
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000101', '286020000000101', '905421000101', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000102', '286020000000102', '905421000102', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000103', '286020000000103', '905421000103', 'physical', 'active', 'lte_m', NULL, NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000004', '89900200000000000104', '286020000000104', '905421000104', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000004', '89900200000000000105', '286020000000105', '905421000105', 'esim', 'active', 'nr_5g', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000004', '89900200000000000106', '286020000000106', '905421000106', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000107', '286020000000107', '905421000107', 'physical', 'active', 'lte_m', NULL, NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000108', '286020000000108', '905421000108', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '15 days', NOW() - INTERVAL '20 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000004', '89900200000000000109', '286020000000109', '905421000109', 'esim', 'active', 'lte', '51000000-0000-0000-0000-000000000005', NOW() - INTERVAL '10 days', NOW() - INTERVAL '15 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000110', '286020000000110', '905421000110', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '8 days', NOW() - INTERVAL '12 days'),
-- Suspended/ordered on Vodafone
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000201', '286020000000201', '905421000201', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '60 days', NOW() - INTERVAL '65 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000004', '89900200000000000202', '286020000000202', '905421000202', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '55 days', NOW() - INTERVAL '60 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', NULL, '89900200000000000301', '286020000000301', '905421000301', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '2 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', NULL, '89900200000000000302', '286020000000302', '905421000302', 'esim', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '1 day'),
-- Terminated
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000003', '89900200000000000401', '286020000000401', '905421000401', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '90 days', NOW() - INTERVAL '100 days')
ON CONFLICT DO NOTHING;

-- Nar Teknoloji - Turk Telekom SIMs (10)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at) VALUES
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000101', '286030000000101', '905551000101', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000102', '286030000000102', '905551000102', 'physical', 'active', 'nr_5g', NULL, NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000103', '286030000000103', '905551000103', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000104', '286030000000104', '905551000104', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000105', '286030000000105', '905551000105', 'physical', 'active', 'nr_5g', NULL, NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000106', '286030000000106', '905551000106', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '60 days', NOW() - INTERVAL '70 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', NULL, '89900300000000000107', '286030000000107', '905551000107', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '4 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '60000000-0000-0000-0000-000000000005', '89900300000000000108', '286030000000108', '905551000108', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '80 days', NOW() - INTERVAL '90 days')
ON CONFLICT DO NOTHING;

-- Bosphorus IoT SIMs (40)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at) VALUES
-- Turkcell SIMs for Bosphorus
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001001', '286010000001001', '905302001001', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001002', '286010000001002', '905302001002', 'physical', 'active', 'nr_5g', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001003', '286010000001003', '905302001003', 'esim', 'active', 'lte', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001004', '286010000001004', '905302001004', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001005', '286010000001005', '905302001005', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001006', '286010000001006', '905302001006', 'physical', 'active', 'nr_5g', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001007', '286010000001007', '905302001007', 'esim', 'active', 'lte', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '15 days', NOW() - INTERVAL '20 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001008', '286010000001008', '905302001008', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000011', NOW() - INTERVAL '10 days', NOW() - INTERVAL '15 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000012', '89900100000000001009', '286010000001009', '905302001009', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000012', NOW() - INTERVAL '42 days', NOW() - INTERVAL '47 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000012', '89900100000000001010', '286010000001010', '905302001010', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000012', NOW() - INTERVAL '38 days', NOW() - INTERVAL '43 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000012', '89900100000000001011', '286010000001011', '905302001011', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000012', NOW() - INTERVAL '33 days', NOW() - INTERVAL '38 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000012', '89900100000000001012', '286010000001012', '905302001012', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000012', NOW() - INTERVAL '28 days', NOW() - INTERVAL '33 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000015', '89900100000000001013', '286010000001013', '905302001013', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '22 days', NOW() - INTERVAL '27 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000015', '89900100000000001014', '286010000001014', '905302001014', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '18 days', NOW() - INTERVAL '23 days'),
-- Vodafone SIMs for Bosphorus
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', '89900200000000001001', '286020000001001', '905422001001', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', '89900200000000001002', '286020000001002', '905422001002', 'physical', 'active', 'nr_5g', NULL, NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', '89900200000000001003', '286020000001003', '905422001003', 'esim', 'active', 'lte', NULL, NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000014', '89900200000000001004', '286020000001004', '905422001004', 'physical', 'active', 'lte', '51000000-0000-0000-0000-000000000013', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000014', '89900200000000001005', '286020000001005', '905422001005', 'physical', 'active', 'nb_iot', '51000000-0000-0000-0000-000000000013', NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000014', '89900200000000001006', '286020000001006', '905422001006', 'physical', 'active', 'lte_m', '51000000-0000-0000-0000-000000000013', NOW() - INTERVAL '15 days', NOW() - INTERVAL '20 days'),
-- Suspended/ordered for Bosphorus
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001015', '286010000001015', '905302001015', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', '89900200000000001007', '286020000001007', '905422001007', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', NULL, '89900100000000001016', '286010000001016', '905302001016', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '3 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', NULL, '89900200000000001008', '286020000001008', '905422001008', 'esim', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '2 days'),
-- Terminated
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000011', '89900100000000001017', '286010000001017', '905302001017', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '70 days', NOW() - INTERVAL '80 days'),
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000002', '60000000-0000-0000-0000-000000000013', '89900200000000001009', '286020000001009', '905422001009', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '60 days', NOW() - INTERVAL '70 days'),
-- Stolen/lost
(gen_random_uuid(), '10000000-0000-0000-0000-000000000002', '20000000-0000-0000-0000-000000000001', '60000000-0000-0000-0000-000000000012', '89900100000000001018', '286010000001018', '905302001018', 'physical', 'stolen_lost', 'nb_iot', NULL, NOW() - INTERVAL '20 days', NOW() - INTERVAL '40 days')
ON CONFLICT DO NOTHING;

-- Additional bulk SIMs to reach 120+ total (30 more for Nar/Turkcell on iot.fleet and m2m.meter)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at)
SELECT
    gen_random_uuid(),
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    CASE WHEN i % 2 = 0 THEN '60000000-0000-0000-0000-000000000001'::uuid ELSE '60000000-0000-0000-0000-000000000002'::uuid END,
    '89900100000000009' || LPAD(i::text, 3, '0'),
    '28601000000' || LPAD((9000+i)::text, 4, '0'),
    '9053010009' || LPAD(i::text, 2, '0'),
    'physical',
    'active',
    CASE WHEN i % 3 = 0 THEN 'lte' WHEN i % 3 = 1 THEN 'nb_iot' ELSE 'lte_m' END,
    CASE WHEN i % 2 = 0 THEN '51000000-0000-0000-0000-000000000001'::uuid ELSE '51000000-0000-0000-0000-000000000003'::uuid END,
    NOW() - (i * INTERVAL '1 day'),
    NOW() - ((i+5) * INTERVAL '1 day')
FROM generate_series(1, 30) AS s(i)
ON CONFLICT DO NOTHING;

-- ============================================================
-- eSIM PROFILES (20+)
-- ============================================================
INSERT INTO esim_profiles (id, sim_id, eid, sm_dp_plus_id, operator_id, profile_state, iccid_on_profile, last_provisioned_at) VALUES
('80000000-0000-0000-0000-000000000001', (SELECT id FROM sims WHERE iccid='89900100000000000104' LIMIT 1), '89049032000000000000000000000001', 'SMDP-TC-001', '20000000-0000-0000-0000-000000000001', 'enabled', '89900100000000000104', NOW() - INTERVAL '45 days'),
('80000000-0000-0000-0000-000000000002', (SELECT id FROM sims WHERE iccid='89900100000000000108' LIMIT 1), '89049032000000000000000000000002', 'SMDP-TC-002', '20000000-0000-0000-0000-000000000001', 'enabled', '89900100000000000108', NOW() - INTERVAL '25 days'),
('80000000-0000-0000-0000-000000000003', (SELECT id FROM sims WHERE iccid='89900100000000000403' LIMIT 1), '89049032000000000000000000000003', 'SMDP-TC-003', '20000000-0000-0000-0000-000000000001', 'disabled', '89900100000000000403', NULL),
('80000000-0000-0000-0000-000000000004', (SELECT id FROM sims WHERE iccid='89900200000000000105' LIMIT 1), '89049032000000000000000000000004', 'SMDP-VF-001', '20000000-0000-0000-0000-000000000002', 'enabled', '89900200000000000105', NOW() - INTERVAL '30 days'),
('80000000-0000-0000-0000-000000000005', (SELECT id FROM sims WHERE iccid='89900200000000000109' LIMIT 1), '89049032000000000000000000000005', 'SMDP-VF-002', '20000000-0000-0000-0000-000000000002', 'enabled', '89900200000000000109', NOW() - INTERVAL '10 days'),
('80000000-0000-0000-0000-000000000006', (SELECT id FROM sims WHERE iccid='89900200000000000302' LIMIT 1), '89049032000000000000000000000006', 'SMDP-VF-003', '20000000-0000-0000-0000-000000000002', 'disabled', '89900200000000000302', NULL),
-- Bosphorus IoT eSIMs
('80000000-0000-0000-0000-000000000011', (SELECT id FROM sims WHERE iccid='89900100000000001003' LIMIT 1), '89049032000000000000000000000011', 'SMDP-TC-011', '20000000-0000-0000-0000-000000000001', 'enabled', '89900100000000001003', NOW() - INTERVAL '35 days'),
('80000000-0000-0000-0000-000000000012', (SELECT id FROM sims WHERE iccid='89900100000000001007' LIMIT 1), '89049032000000000000000000000012', 'SMDP-TC-012', '20000000-0000-0000-0000-000000000001', 'enabled', '89900100000000001007', NOW() - INTERVAL '15 days'),
('80000000-0000-0000-0000-000000000013', (SELECT id FROM sims WHERE iccid='89900200000000001003' LIMIT 1), '89049032000000000000000000000013', 'SMDP-VF-011', '20000000-0000-0000-0000-000000000002', 'enabled', '89900200000000001003', NOW() - INTERVAL '30 days'),
('80000000-0000-0000-0000-000000000014', (SELECT id FROM sims WHERE iccid='89900200000000001008' LIMIT 1), '89049032000000000000000000000014', 'SMDP-VF-012', '20000000-0000-0000-0000-000000000002', 'disabled', '89900200000000001008', NULL)
ON CONFLICT DO NOTHING;

-- Update esim_profile_id on SIMs
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000001' WHERE iccid = '89900100000000000104';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000002' WHERE iccid = '89900100000000000108';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000003' WHERE iccid = '89900100000000000403';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000004' WHERE iccid = '89900200000000000105';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000005' WHERE iccid = '89900200000000000109';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000006' WHERE iccid = '89900200000000000302';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000011' WHERE iccid = '89900100000000001003';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000012' WHERE iccid = '89900100000000001007';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000013' WHERE iccid = '89900200000000001003';
UPDATE sims SET esim_profile_id = '80000000-0000-0000-0000-000000000014' WHERE iccid = '89900200000000001008';

-- ============================================================
-- SESSIONS (50+ active, 200+ historical)
-- ============================================================

-- Active sessions for Nar Teknoloji SIMs
INSERT INTO sessions (id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip, calling_station_id, called_station_id, rat_type, session_state, auth_method, protocol_type, acct_session_id, started_at, bytes_in, bytes_out, packets_in, packets_out, last_interim_at)
SELECT
    gen_random_uuid(),
    s.id,
    s.tenant_id,
    s.operator_id,
    s.apn_id,
    '10.0.0.1'::inet,
    ('10.1.' || ((row_number() OVER()) / 256) || '.' || ((row_number() OVER()) % 256))::inet,
    s.msisdn,
    a.name,
    s.rat_type,
    'active',
    'eap_sim',
    CASE WHEN s.rat_type = 'nr_5g' THEN '5g_sba' WHEN s.rat_type IN ('nb_iot','lte_m') THEN 'diameter' ELSE 'radius' END,
    'ACCT-NAR-' || LPAD((row_number() OVER())::text, 6, '0'),
    NOW() - (random() * INTERVAL '12 hours'),
    (random() * 100000000)::bigint,
    (random() * 50000000)::bigint,
    (random() * 100000)::bigint,
    (random() * 50000)::bigint,
    NOW() - (random() * INTERVAL '30 minutes')
FROM sims s
JOIN apns a ON s.apn_id = a.id
WHERE s.tenant_id = '10000000-0000-0000-0000-000000000001'
  AND s.state = 'active'
  AND s.apn_id IS NOT NULL
LIMIT 35;

-- Active sessions for Bosphorus IoT SIMs
INSERT INTO sessions (id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip, calling_station_id, called_station_id, rat_type, session_state, auth_method, protocol_type, acct_session_id, started_at, bytes_in, bytes_out, packets_in, packets_out, last_interim_at)
SELECT
    gen_random_uuid(),
    s.id,
    s.tenant_id,
    s.operator_id,
    s.apn_id,
    '10.0.0.2'::inet,
    ('10.10.' || ((row_number() OVER()) / 256) || '.' || ((row_number() OVER()) % 256))::inet,
    s.msisdn,
    a.name,
    s.rat_type,
    'active',
    'eap_aka',
    CASE WHEN s.rat_type = 'nr_5g' THEN '5g_sba' WHEN s.rat_type IN ('nb_iot','lte_m') THEN 'diameter' ELSE 'radius' END,
    'ACCT-BIO-' || LPAD((row_number() OVER())::text, 6, '0'),
    NOW() - (random() * INTERVAL '8 hours'),
    (random() * 80000000)::bigint,
    (random() * 40000000)::bigint,
    (random() * 80000)::bigint,
    (random() * 40000)::bigint,
    NOW() - (random() * INTERVAL '20 minutes')
FROM sims s
JOIN apns a ON s.apn_id = a.id
WHERE s.tenant_id = '10000000-0000-0000-0000-000000000002'
  AND s.state = 'active'
  AND s.apn_id IS NOT NULL
LIMIT 20;

-- Historical sessions (ended, spanning 30 days)
INSERT INTO sessions (id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip, calling_station_id, rat_type, session_state, auth_method, protocol_type, acct_session_id, started_at, ended_at, terminate_cause, bytes_in, bytes_out, packets_in, packets_out)
SELECT
    gen_random_uuid(),
    s.id,
    s.tenant_id,
    s.operator_id,
    s.apn_id,
    '10.0.0.1'::inet,
    ('10.1.' || ((i*3 + row_number() OVER()) / 256) || '.' || ((i*3 + row_number() OVER()) % 256))::inet,
    s.msisdn,
    s.rat_type,
    'closed',
    'eap_sim',
    'radius',
    'HIST-' || LPAD(i::text, 3, '0') || '-' || LPAD((row_number() OVER())::text, 4, '0'),
    NOW() - (i * INTERVAL '1 day') - (random() * INTERVAL '12 hours'),
    NOW() - (i * INTERVAL '1 day') - (random() * INTERVAL '12 hours') + (random() * INTERVAL '4 hours'),
    (ARRAY['User-Request','Lost-Carrier','Idle-Timeout','Session-Timeout','Admin-Reset'])[1 + (random()*4)::int],
    (random() * 200000000)::bigint,
    (random() * 100000000)::bigint,
    (random() * 200000)::bigint,
    (random() * 100000)::bigint
FROM sims s
CROSS JOIN generate_series(1, 7) AS gs(i)
WHERE s.tenant_id = '10000000-0000-0000-0000-000000000001'
  AND s.state = 'active'
  AND s.apn_id IS NOT NULL
LIMIT 200;

-- ============================================================
-- CDRs (500+ records, spanning 30 days)
-- ============================================================
INSERT INTO cdrs (session_id, sim_id, tenant_id, operator_id, apn_id, rat_type, record_type, bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp)
SELECT
    sess.id,
    sess.sim_id,
    sess.tenant_id,
    sess.operator_id,
    sess.apn_id,
    sess.rat_type,
    (ARRAY['interim','stop'])[1 + (random())::int],
    (random() * 50000000)::bigint,
    (random() * 25000000)::bigint,
    (random() * 3600)::int,
    round((random() * 5.0)::numeric, 4),
    round((random() * 3.0)::numeric, 4),
    round((random() * 0.1)::numeric, 4),
    CASE
        WHEN sess.rat_type = 'nr_5g' THEN 1.5
        WHEN sess.rat_type = 'lte' THEN 1.0
        WHEN sess.rat_type = 'lte_m' THEN 0.8
        WHEN sess.rat_type = 'nb_iot' THEN 0.5
        ELSE 1.0
    END,
    sess.started_at + (random() * COALESCE(sess.ended_at - sess.started_at, INTERVAL '4 hours'))
FROM sessions sess
WHERE sess.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
LIMIT 500;

-- Additional CDRs to fill 30 days of chart data
INSERT INTO cdrs (session_id, sim_id, tenant_id, operator_id, apn_id, rat_type, record_type, bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp)
SELECT
    sess.id,
    sess.sim_id,
    sess.tenant_id,
    sess.operator_id,
    sess.apn_id,
    sess.rat_type,
    'stop',
    (random() * 80000000)::bigint,
    (random() * 40000000)::bigint,
    (random() * 7200)::int,
    round((random() * 8.0)::numeric, 4),
    round((random() * 5.0)::numeric, 4),
    round((random() * 0.15)::numeric, 4),
    1.0,
    NOW() - ((d * 24 + (random()*23)::int) * INTERVAL '1 hour')
FROM generate_series(0, 29) AS gs(d)
CROSS JOIN LATERAL (
    SELECT id, sim_id, tenant_id, operator_id, apn_id, rat_type
    FROM sessions
    WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
    ORDER BY random()
    LIMIT 5
) sess
ON CONFLICT DO NOTHING;

-- ============================================================
-- POLICY ROLLOUTS
-- ============================================================
INSERT INTO policy_rollouts (id, policy_version_id, previous_version_id, strategy, stages, current_stage, total_sims, migrated_sims, state, started_at, created_by) VALUES
('90000000-0000-0000-0000-000000000001', '51000000-0000-0000-0000-000000000002', '51000000-0000-0000-0000-000000000001', 'canary',
 '[{"percentage":1,"target_count":1,"migrated":1,"started_at":"2026-03-20T10:00:00Z"},{"percentage":10,"target_count":5,"migrated":5,"started_at":"2026-03-21T10:00:00Z"},{"percentage":100,"target_count":45,"migrated":0}]',
 2, 45, 6, 'in_progress', NOW() - INTERVAL '3 days', '40000000-0000-0000-0000-000000000003'),
('90000000-0000-0000-0000-000000000002', '51000000-0000-0000-0000-000000000005', '51000000-0000-0000-0000-000000000004', 'canary',
 '[{"percentage":1,"target_count":1,"migrated":1},{"percentage":10,"target_count":2,"migrated":2},{"percentage":100,"target_count":15,"migrated":15}]',
 3, 15, 15, 'completed', NOW() - INTERVAL '12 days', '40000000-0000-0000-0000-000000000003')
ON CONFLICT DO NOTHING;

-- ============================================================
-- POLICY ASSIGNMENTS (link some SIMs to policy versions)
-- ============================================================
INSERT INTO policy_assignments (id, sim_id, policy_version_id, rollout_id, assigned_at, coa_status)
SELECT
    gen_random_uuid(),
    s.id,
    s.policy_version_id,
    CASE WHEN s.policy_version_id = '51000000-0000-0000-0000-000000000002' THEN '90000000-0000-0000-0000-000000000001'::uuid ELSE NULL END,
    s.activated_at + INTERVAL '1 hour',
    'acked'
FROM sims s
WHERE s.policy_version_id IS NOT NULL
  AND s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
ON CONFLICT DO NOTHING;

-- ============================================================
-- JOBS (20+ with different types and states)
-- ============================================================
INSERT INTO jobs (id, tenant_id, type, state, priority, payload, total_items, processed_items, failed_items, progress_pct, started_at, completed_at, created_at, created_by) VALUES
-- Nar Teknoloji jobs
('A0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'completed', 5,
 '{"filename":"fleet_sims_batch1.csv","operator":"Turkcell","apn":"iot.fleet"}', 50, 48, 2, 100.00,
 NOW() - INTERVAL '20 days', NOW() - INTERVAL '20 days' + INTERVAL '5 minutes', NOW() - INTERVAL '20 days', '40000000-0000-0000-0000-000000000002'),
('A0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'completed', 5,
 '{"filename":"meter_sims.csv","operator":"Turkcell","apn":"m2m.meter"}', 30, 30, 0, 100.00,
 NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days' + INTERVAL '3 minutes', NOW() - INTERVAL '15 days', '40000000-0000-0000-0000-000000000002'),
('A0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'sim_state_change', 'completed', 3,
 '{"action":"suspend","reason":"Fatura ödenmedi","sim_count":3}', 3, 3, 0, 100.00,
 NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days' + INTERVAL '30 seconds', NOW() - INTERVAL '10 days', '40000000-0000-0000-0000-000000000001'),
('A0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'policy_rollout', 'running', 7,
 '{"policy":"Fleet Standart QoS","version":2,"strategy":"canary","stage":2}', 45, 6, 0, 13.33,
 NOW() - INTERVAL '3 days', NULL, NOW() - INTERVAL '3 days', '40000000-0000-0000-0000-000000000003'),
('A0000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'failed', 5,
 '{"filename":"bad_data.csv","operator":"Turkcell","error":"invalid CSV format"}', 100, 0, 100, 0.00,
 NOW() - INTERVAL '8 days', NOW() - INTERVAL '8 days' + INTERVAL '10 seconds', NOW() - INTERVAL '8 days', '40000000-0000-0000-0000-000000000002'),
('A0000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001', 'ip_pool_reclaim', 'completed', 2,
 '{"pool":"Fleet IPv4 Pool","reclaimed":12}', 12, 12, 0, 100.00,
 NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days' + INTERVAL '1 minute', NOW() - INTERVAL '7 days', NULL),
('A0000000-0000-0000-0000-000000000007', '10000000-0000-0000-0000-000000000001', 'cdr_export', 'completed', 4,
 '{"format":"csv","date_range":"2026-02-01 to 2026-02-28","tenant":"Nar Teknoloji"}', 1500, 1500, 0, 100.00,
 NOW() - INTERVAL '5 days', NOW() - INTERVAL '5 days' + INTERVAL '2 minutes', NOW() - INTERVAL '5 days', '40000000-0000-0000-0000-000000000004'),
('A0000000-0000-0000-0000-000000000008', '10000000-0000-0000-0000-000000000001', 'ota_bulk_command', 'running', 6,
 '{"command_type":"UPDATE_FILE","target_count":10}', 10, 7, 1, 70.00,
 NOW() - INTERVAL '2 hours', NULL, NOW() - INTERVAL '2 hours', '40000000-0000-0000-0000-000000000002'),
('A0000000-0000-0000-0000-000000000009', '10000000-0000-0000-0000-000000000001', 'sim_state_change', 'queued', 5,
 '{"action":"activate","sim_count":5}', 5, 0, 0, 0.00,
 NULL, NULL, NOW() - INTERVAL '30 minutes', '40000000-0000-0000-0000-000000000002'),
('A0000000-0000-0000-0000-000000000010', '10000000-0000-0000-0000-000000000001', 'compliance_report', 'completed', 3,
 '{"type":"KVKK","period":"2026-Q1"}', 1, 1, 0, 100.00,
 NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days' + INTERVAL '45 seconds', NOW() - INTERVAL '2 days', '40000000-0000-0000-0000-000000000001'),
-- Bosphorus IoT jobs
('A0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', 'sim_bulk_import', 'completed', 5,
 '{"filename":"city_sensors.csv","operator":"Turkcell","apn":"iot.city"}', 25, 25, 0, 100.00,
 NOW() - INTERVAL '18 days', NOW() - INTERVAL '18 days' + INTERVAL '4 minutes', NOW() - INTERVAL '18 days', '40000000-0000-0000-0000-000000000012'),
('A0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', 'sim_bulk_import', 'completed', 5,
 '{"filename":"agri_sensors.csv","operator":"Turkcell","apn":"m2m.agri"}', 15, 14, 1, 100.00,
 NOW() - INTERVAL '14 days', NOW() - INTERVAL '14 days' + INTERVAL '2 minutes', NOW() - INTERVAL '14 days', '40000000-0000-0000-0000-000000000012'),
('A0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', 'policy_rollout', 'completed', 7,
 '{"policy":"Akilli Sehir QoS","version":1,"strategy":"immediate"}', 20, 20, 0, 100.00,
 NOW() - INTERVAL '16 days', NOW() - INTERVAL '16 days' + INTERVAL '90 seconds', NOW() - INTERVAL '16 days', '40000000-0000-0000-0000-000000000013'),
('A0000000-0000-0000-0000-000000000014', '10000000-0000-0000-0000-000000000002', 'esim_profile_switch', 'completed', 6,
 '{"from_operator":"Turkcell","to_operator":"Vodafone","sim_count":3}', 3, 3, 0, 100.00,
 NOW() - INTERVAL '9 days', NOW() - INTERVAL '9 days' + INTERVAL '5 minutes', NOW() - INTERVAL '9 days', '40000000-0000-0000-0000-000000000012'),
('A0000000-0000-0000-0000-000000000015', '10000000-0000-0000-0000-000000000002', 'sim_state_change', 'running', 5,
 '{"action":"suspend","reason":"Proje sonlandi","sim_count":2}', 2, 1, 0, 50.00,
 NOW() - INTERVAL '30 minutes', NULL, NOW() - INTERVAL '35 minutes', '40000000-0000-0000-0000-000000000011'),
('A0000000-0000-0000-0000-000000000016', '10000000-0000-0000-0000-000000000002', 'cdr_export', 'queued', 4,
 '{"format":"csv","date_range":"2026-03-01 to 2026-03-23"}', 0, 0, 0, 0.00,
 NULL, NULL, NOW() - INTERVAL '15 minutes', '40000000-0000-0000-0000-000000000014'),
('A0000000-0000-0000-0000-000000000017', '10000000-0000-0000-0000-000000000002', 'ip_pool_reclaim', 'completed', 2,
 '{"pool":"Energy Pool","reclaimed":5}', 5, 5, 0, 100.00,
 NOW() - INTERVAL '4 days', NOW() - INTERVAL '4 days' + INTERVAL '30 seconds', NOW() - INTERVAL '4 days', NULL),
('A0000000-0000-0000-0000-000000000018', '10000000-0000-0000-0000-000000000001', 'sim_purge', 'completed', 1,
 '{"retention_days":90,"purged_count":0}', 0, 0, 0, 100.00,
 NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day' + INTERVAL '5 seconds', NOW() - INTERVAL '1 day', NULL),
('A0000000-0000-0000-0000-000000000019', '10000000-0000-0000-0000-000000000001', 'audit_export', 'completed', 4,
 '{"format":"json","date_range":"2026-03-01 to 2026-03-15"}', 500, 500, 0, 100.00,
 NOW() - INTERVAL '6 days', NOW() - INTERVAL '6 days' + INTERVAL '3 minutes', NOW() - INTERVAL '6 days', '40000000-0000-0000-0000-000000000001'),
('A0000000-0000-0000-0000-000000000020', '10000000-0000-0000-0000-000000000002', 'ota_bulk_command', 'completed', 6,
 '{"command_type":"INSTALL_APPLET","target_count":5}', 5, 5, 0, 100.00,
 NOW() - INTERVAL '11 days', NOW() - INTERVAL '11 days' + INTERVAL '8 minutes', NOW() - INTERVAL '11 days', '40000000-0000-0000-0000-000000000012')
ON CONFLICT DO NOTHING;

-- ============================================================
-- NOTIFICATIONS (50+ with unread/read mix)
-- ============================================================
INSERT INTO notifications (id, tenant_id, user_id, event_type, scope_type, title, body, severity, channels_sent, state, read_at, created_at) VALUES
-- Nar Teknoloji notifications
('B0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'ip_pool_warning', 'apn', 'IP Pool Uyarisi: Camera IPv4 Pool', 'Camera IPv4 Pool havuzu %91 kapasiteye ulasti. Acil genisleme gerekli.', 'critical', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '2 hours'),
('B0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'ip_pool_warning', 'apn', 'IP Pool Uyarisi: Meter IPv4 Pool', 'Meter IPv4 Pool havuzu %85 kapasiteye ulasti.', 'warning', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '4 hours'),
('B0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 'sim_state_change', 'sim', 'SIM Durumu Degisti', 'SIM 89900100000000000301 ACTIVE''den SUSPENDED''a gecti.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '8 hours', NOW() - INTERVAL '10 hours'),
('B0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 'sim_stolen_lost', 'sim', 'SIM Calinti/Kayip Bildirimi', 'SIM 89900100000000000601 calinti/kayip olarak isaretlendi. Oturum sonlandirildi.', 'critical', ARRAY['in_app','email','webhook'], 'read', NOW() - INTERVAL '28 hours', NOW() - INTERVAL '30 hours'),
('B0000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000003', 'policy_rollout_started', 'policy', 'Politika Yayilimi Basladi', 'Fleet Standart QoS v2 canary yayilimi basladi. Asamal: %1 → %10 → %100', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '2 days', NOW() - INTERVAL '3 days'),
('B0000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000003', 'policy_rollout_stage', 'policy', 'Politika Yayilim Asamasi 2', 'Fleet Standart QoS v2 %10 asamasina gecti. 5 SIM guncellendi.', 'info', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '1 day'),
('B0000000-0000-0000-0000-000000000007', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'job_completed', 'system', 'Toplu SIM Aktarimi Tamamlandi', 'fleet_sims_batch1.csv: 48/50 basarili, 2 hata.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '18 days', NOW() - INTERVAL '20 days'),
('B0000000-0000-0000-0000-000000000008', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 'job_failed', 'system', 'Toplu Aktarim Hatasi', 'bad_data.csv: Gecersiz CSV formati. Hic kayit islenmedi.', 'error', ARRAY['in_app','email'], 'read', NOW() - INTERVAL '7 days', NOW() - INTERVAL '8 days'),
('B0000000-0000-0000-0000-000000000009', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000005', 'operator_degraded', 'operator', 'Operator Durumu: Turk Telekom DEGRADED', 'Turk Telekom operatoru saglik kontrolunde basarisiz oldu. Circuit breaker devrede.', 'warning', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '6 hours'),
('B0000000-0000-0000-0000-000000000010', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000005', 'sla_violation', 'operator', 'SLA Ihlali: Turk Telekom', 'Turk Telekom son 24 saatte %98.5 uptime gosteriyor. Hedef: %99.9', 'warning', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '5 hours'),
('B0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000004', 'anomaly_detected', 'sim', 'Anomali Tespit Edildi: Veri Spiki', 'SIM 89900100000000000103 son 1 saatte normalin 50x veri kullandi.', 'critical', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '1 hour'),
('B0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000004', 'compliance_report', 'system', 'KVKK Raporu Hazir', '2026 Q1 KVKK uyumluluk raporu olusturuldu. Indirmek icin tiklayin.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '1 day', NOW() - INTERVAL '2 days'),
('B0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'user_login', 'system', 'Yeni Giris Bildirimi', 'elif.kaya@nar.com.tr 192.168.1.100 adresinden giris yapti.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '30 minutes', NOW() - INTERVAL '35 minutes'),
('B0000000-0000-0000-0000-000000000014', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'api_key_expiring', 'system', 'API Anahtari Süresi Doluyor', 'Filo API anahtarinin suresi 7 gun icinde dolacak.', 'warning', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '1 day'),
('B0000000-0000-0000-0000-000000000015', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 'ota_command_failed', 'sim', 'OTA Komut Hatasi', 'SIM 89900100000000000201''e gonderilen UPDATE_FILE komutu basarisiz oldu.', 'error', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '3 hours'),
-- Bosphorus IoT notifications
('B0000000-0000-0000-0000-000000000021', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'job_completed', 'system', 'Sensor Aktarimi Tamamlandi', 'city_sensors.csv: 25/25 basarili.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '17 days', NOW() - INTERVAL '18 days'),
('B0000000-0000-0000-0000-000000000022', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'ip_pool_warning', 'apn', 'IP Pool Uyarisi: Energy Pool', 'Energy Pool havuzu %89 kapasiteye ulasti.', 'warning', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '3 hours'),
('B0000000-0000-0000-0000-000000000023', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000012', 'esim_switch_completed', 'sim', 'eSIM Profil Degisikligi', '3 SIM basariyla Turkcell''den Vodafone''a aktarildi.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '8 days', NOW() - INTERVAL '9 days'),
('B0000000-0000-0000-0000-000000000024', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000013', 'policy_activated', 'policy', 'Politika Aktif', 'Akilli Sehir QoS v1 basariyla aktive edildi.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '16 days', NOW() - INTERVAL '18 days'),
('B0000000-0000-0000-0000-000000000025', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'sim_stolen_lost', 'sim', 'SIM Calinti Bildirimi', 'SIM 89900100000000001018 calinti olarak isaretlendi.', 'critical', ARRAY['in_app','email'], 'read', NOW() - INTERVAL '19 days', NOW() - INTERVAL '20 days'),
('B0000000-0000-0000-0000-000000000026', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000014', 'anomaly_detected', 'sim', 'Anomali: Auth Flood', 'SIM 89900200000000001002 icin 5 dakikada 500+ auth denemesi tespit edildi.', 'critical', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '5 hours'),
('B0000000-0000-0000-0000-000000000027', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'operator_degraded', 'operator', 'Turk Telekom Degraded', 'Turk Telekom operatoru degraded durumda. Etkilenen SIM''ler icin failover aktif.', 'warning', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '6 hours')
ON CONFLICT DO NOTHING;

-- Generate more notifications to reach 50+
INSERT INTO notifications (tenant_id, user_id, event_type, scope_type, title, body, severity, channels_sent, state, read_at, created_at)
SELECT
    '10000000-0000-0000-0000-000000000001',
    (ARRAY['40000000-0000-0000-0000-000000000001','40000000-0000-0000-0000-000000000002','40000000-0000-0000-0000-000000000003','40000000-0000-0000-0000-000000000004','40000000-0000-0000-0000-000000000005']::uuid[])[1 + (random()*4)::int],
    (ARRAY['session_started','session_ended','sim_state_change','usage_threshold','heartbeat_ok'])[1 + (random()*4)::int],
    (ARRAY['sim','apn','operator','system'])[1 + (random()*3)::int],
    'Sistem Bildirimi #' || i,
    'Otomatik sistem bildirimi. Detaylar icin tiklayin.',
    (ARRAY['info','warning','info','info'])[1 + (random()*3)::int],
    ARRAY['in_app'],
    CASE WHEN random() > 0.3 THEN 'read' ELSE 'unread' END,
    CASE WHEN random() > 0.3 THEN NOW() - (i * INTERVAL '2 hours') ELSE NULL END,
    NOW() - (i * INTERVAL '3 hours')
FROM generate_series(1, 25) AS s(i);

-- ============================================================
-- API KEYS (3+ per tenant)
-- ============================================================
INSERT INTO api_keys (id, tenant_id, name, key_prefix, key_hash, scopes, rate_limit_per_minute, rate_limit_per_hour, expires_at, last_used_at, usage_count, created_by) VALUES
-- Nar Teknoloji API keys
('C0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'Filo Yonetim API', 'arg_fl01', '$2a$12$randomhash1forfleetapikey00000000000000000000000000', '["sims:read","sims:write","sessions:read"]', 500, 15000, NOW() + INTERVAL '30 days', NOW() - INTERVAL '1 hour', 12450, '40000000-0000-0000-0000-000000000001'),
('C0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'Analytics API', 'arg_an01', '$2a$12$randomhash2foranalyticsapikey000000000000000000000', '["cdrs:read","analytics:read","sessions:read"]', 1000, 30000, NOW() + INTERVAL '90 days', NOW() - INTERVAL '3 hours', 8200, '40000000-0000-0000-0000-000000000001'),
('C0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'Webhook Entegrasyonu', 'arg_wh01', '$2a$12$randomhash3forwebhookapikey000000000000000000000', '["notifications:read","events:subscribe"]', 200, 6000, NOW() + INTERVAL '180 days', NOW() - INTERVAL '12 hours', 3400, '40000000-0000-0000-0000-000000000006'),
('C0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'Test API (suresi dolmus)', 'arg_ts01', '$2a$12$randomhash4fortestexpiredkey0000000000000000000000', '["*"]', 100, 3000, NOW() - INTERVAL '10 days', NOW() - INTERVAL '11 days', 150, '40000000-0000-0000-0000-000000000001'),
-- Bosphorus IoT API keys
('C0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', 'Sehir Sensor API', 'arg_cs01', '$2a$12$randomhash5forcitysensorapi000000000000000000000', '["sims:read","sessions:read","cdrs:read"]', 800, 24000, NOW() + INTERVAL '60 days', NOW() - INTERVAL '2 hours', 6800, '40000000-0000-0000-0000-000000000011'),
('C0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', 'Tarim IoT API', 'arg_ag01', '$2a$12$randomhash6foragriiotapikey000000000000000000000', '["sims:read","sims:write"]', 300, 9000, NOW() + INTERVAL '120 days', NOW() - INTERVAL '5 hours', 2100, '40000000-0000-0000-0000-000000000011'),
('C0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', 'Enerji Izleme API', 'arg_en01', '$2a$12$randomhash7forenergymonitorapi0000000000000000000', '["sims:read","analytics:read","notifications:read"]', 500, 15000, NOW() + INTERVAL '45 days', NOW() - INTERVAL '8 hours', 4500, '40000000-0000-0000-0000-000000000011')
ON CONFLICT DO NOTHING;

-- ============================================================
-- OTA COMMANDS (10+)
-- ============================================================
INSERT INTO ota_commands (id, tenant_id, sim_id, command_type, channel, status, security_mode, payload, response_data, error_message, sent_at, delivered_at, executed_at, completed_at, created_by, created_at) VALUES
('D0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000101' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'confirmed', 'kic_kid', '{"file_id":"EF_FPLMN","data":"286020"}', '{"sw1":"90","sw2":"00"}', NULL,
 NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days' + INTERVAL '30 seconds', NOW() - INTERVAL '10 days' + INTERVAL '45 seconds', NOW() - INTERVAL '10 days' + INTERVAL '1 minute',
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '10 days'),
('D0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000102' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'confirmed', 'kic', '{"file_id":"EF_FPLMN","data":"286020"}', '{"sw1":"90","sw2":"00"}', NULL,
 NOW() - INTERVAL '9 days', NOW() - INTERVAL '9 days' + INTERVAL '25 seconds', NOW() - INTERVAL '9 days' + INTERVAL '40 seconds', NOW() - INTERVAL '9 days' + INTERVAL '55 seconds',
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '9 days'),
('D0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000201' LIMIT 1),
 'INSTALL_APPLET', 'bip', 'failed', 'kic_kid', '{"applet_id":"A0000000B2030101","cap_url":"https://ota.nar.com.tr/apps/meter-v2.cap"}', NULL, 'BIP baglantisi kurulamadi. Cihaz erisim disi.',
 NOW() - INTERVAL '3 hours', NULL, NULL, NOW() - INTERVAL '2 hours' - INTERVAL '50 minutes',
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '3 hours'),
('D0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000103' LIMIT 1),
 'READ_FILE', 'sms_pp', 'confirmed', 'none', '{"file_id":"EF_ICCID"}', '{"sw1":"90","sw2":"00","data":"89900100000000000103"}', NULL,
 NOW() - INTERVAL '5 days', NOW() - INTERVAL '5 days' + INTERVAL '20 seconds', NOW() - INTERVAL '5 days' + INTERVAL '35 seconds', NOW() - INTERVAL '5 days' + INTERVAL '50 seconds',
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '5 days'),
('D0000000-0000-0000-0000-000000000005', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000104' LIMIT 1),
 'SIM_TOOLKIT', 'sms_pp', 'delivered', 'kid', '{"stk_command":"REFRESH","qualifier":"full_file_change"}', NULL, NULL,
 NOW() - INTERVAL '1 hour', NOW() - INTERVAL '1 hour' + INTERVAL '15 seconds', NULL, NULL,
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '1 hour'),
('D0000000-0000-0000-0000-000000000006', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000105' LIMIT 1),
 'DELETE_APPLET', 'sms_pp', 'executed', 'kic_kid', '{"applet_id":"A0000000B2030100"}', '{"sw1":"90","sw2":"00"}', NULL,
 NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days' + INTERVAL '30 seconds', NOW() - INTERVAL '7 days' + INTERVAL '45 seconds', NULL,
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '7 days'),
('D0000000-0000-0000-0000-000000000007', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000106' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'queued', 'kic', '{"file_id":"EF_OPLMN","data":"28601"}', NULL, NULL,
 NULL, NULL, NULL, NULL,
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '30 minutes'),
('D0000000-0000-0000-0000-000000000008', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000107' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'sent', 'kic_kid', '{"file_id":"EF_FPLMN","data":"286030"}', NULL, NULL,
 NOW() - INTERVAL '15 minutes', NULL, NULL, NULL,
 '40000000-0000-0000-0000-000000000002', NOW() - INTERVAL '20 minutes'),
-- Bosphorus IoT OTA commands
('D0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002',
 (SELECT id FROM sims WHERE iccid='89900100000000001001' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'confirmed', 'kic_kid', '{"file_id":"EF_FPLMN","data":"286020"}', '{"sw1":"90","sw2":"00"}', NULL,
 NOW() - INTERVAL '11 days', NOW() - INTERVAL '11 days' + INTERVAL '30 seconds', NOW() - INTERVAL '11 days' + INTERVAL '45 seconds', NOW() - INTERVAL '11 days' + INTERVAL '1 minute',
 '40000000-0000-0000-0000-000000000012', NOW() - INTERVAL '11 days'),
('D0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002',
 (SELECT id FROM sims WHERE iccid='89900100000000001002' LIMIT 1),
 'INSTALL_APPLET', 'bip', 'confirmed', 'kic_kid', '{"applet_id":"A0000000C1020101","cap_url":"https://ota.bosphorus-iot.com/apps/city-sensor-v1.cap"}', '{"sw1":"90","sw2":"00"}', NULL,
 NOW() - INTERVAL '11 days', NOW() - INTERVAL '11 days' + INTERVAL '2 minutes', NOW() - INTERVAL '11 days' + INTERVAL '3 minutes', NOW() - INTERVAL '11 days' + INTERVAL '4 minutes',
 '40000000-0000-0000-0000-000000000012', NOW() - INTERVAL '11 days'),
('D0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002',
 (SELECT id FROM sims WHERE iccid='89900100000000001009' LIMIT 1),
 'UPDATE_FILE', 'sms_pp', 'failed', 'kid', '{"file_id":"EF_OPLMN","data":"28602"}', NULL, 'SMS gonderimi basarisiz. Cihaz kapsama alaninda degil.',
 NOW() - INTERVAL '5 days', NULL, NULL, NOW() - INTERVAL '5 days' + INTERVAL '5 minutes',
 '40000000-0000-0000-0000-000000000012', NOW() - INTERVAL '5 days')
ON CONFLICT DO NOTHING;

-- ============================================================
-- SIM SEGMENTS (5+)
-- ============================================================
INSERT INTO sim_segments (id, tenant_id, name, filter_definition, created_by) VALUES
-- Nar Teknoloji segments
('E0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', 'Aktif Filo SIM''leri',
 '{"operator":"Turkcell","apn":"iot.fleet","state":"active"}', '40000000-0000-0000-0000-000000000002'),
('E0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', 'NB-IoT Sayaclar',
 '{"rat_type":"nb_iot","apn":"m2m.meter","state":"active"}', '40000000-0000-0000-0000-000000000002'),
('E0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', 'Askiya Alinan SIM''ler',
 '{"state":"suspended"}', '40000000-0000-0000-0000-000000000001'),
('E0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', 'eSIM Profilleri',
 '{"sim_type":"esim","state":"active"}', '40000000-0000-0000-0000-000000000002'),
-- Bosphorus IoT segments
('E0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', 'Sehir Sensorleri',
 '{"apn":"iot.city","state":"active"}', '40000000-0000-0000-0000-000000000012'),
('E0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', 'Tarim Sensorleri',
 '{"apn":"m2m.agri","state":"active","rat_type":"nb_iot"}', '40000000-0000-0000-0000-000000000012'),
('E0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', 'Vodafone SIM''leri',
 '{"operator":"Vodafone TR","state":"active"}', '40000000-0000-0000-0000-000000000012')
ON CONFLICT DO NOTHING;

-- ============================================================
-- MSISDN POOL (50+)
-- ============================================================
INSERT INTO msisdn_pool (tenant_id, operator_id, msisdn, state, created_at)
SELECT
    '10000000-0000-0000-0000-000000000001',
    '20000000-0000-0000-0000-000000000001',
    '9053019' || LPAD(i::text, 5, '0'),
    CASE WHEN i <= 40 THEN 'available' WHEN i <= 45 THEN 'reserved' ELSE 'assigned' END,
    NOW() - INTERVAL '30 days'
FROM generate_series(10001, 10060) AS s(i)
ON CONFLICT DO NOTHING;

-- ============================================================
-- OPERATOR HEALTH LOGS (recent entries)
-- ============================================================
INSERT INTO operator_health_logs (operator_id, checked_at, status, latency_ms, error_message, circuit_state)
SELECT
    op_id::uuid,
    NOW() - (i * INTERVAL '30 seconds'),
    CASE
        WHEN op_id = '20000000-0000-0000-0000-000000000003' AND i < 5 THEN 'degraded'
        WHEN op_id = '20000000-0000-0000-0000-000000000003' AND i < 10 THEN 'unhealthy'
        ELSE 'healthy'
    END,
    CASE
        WHEN op_id = '20000000-0000-0000-0000-000000000003' THEN (50 + random() * 500)::int
        ELSE (5 + random() * 30)::int
    END,
    CASE
        WHEN op_id = '20000000-0000-0000-0000-000000000003' AND i < 10 THEN 'Connection timeout after 3000ms'
        ELSE NULL
    END,
    CASE
        WHEN op_id = '20000000-0000-0000-0000-000000000003' AND i < 5 THEN 'half_open'
        WHEN op_id = '20000000-0000-0000-0000-000000000003' AND i < 10 THEN 'open'
        ELSE 'closed'
    END
FROM (VALUES
    ('20000000-0000-0000-0000-000000000001'),
    ('20000000-0000-0000-0000-000000000002'),
    ('20000000-0000-0000-0000-000000000003')
) AS ops(op_id)
CROSS JOIN generate_series(1, 20) AS gs(i);

-- ============================================================
-- ANOMALIES (variety of types and states)
-- ============================================================
INSERT INTO anomalies (id, tenant_id, sim_id, type, severity, state, details, source, detected_at, acknowledged_at, resolved_at) VALUES
('F0000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000103' LIMIT 1),
 'data_spike', 'critical', 'open',
 '{"expected_mb":50,"actual_mb":2500,"ratio":50,"period":"1h","apn":"iot.fleet"}',
 'anomaly_detector', NOW() - INTERVAL '1 hour', NULL, NULL),
('F0000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000000301' LIMIT 1),
 'sim_cloning', 'critical', 'acknowledged',
 '{"concurrent_sessions":3,"different_nas":["10.0.0.1","10.0.0.5","10.0.0.9"],"evidence":"3 farkli NAS''tan eşzamanlı oturum"}',
 'session_monitor', NOW() - INTERVAL '3 days', NOW() - INTERVAL '2 days', NULL),
('F0000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', NULL,
 'auth_flood', 'high', 'resolved',
 '{"attempts":1500,"period":"5m","source_nas":"10.0.0.1","threshold":100}',
 'auth_monitor', NOW() - INTERVAL '7 days', NOW() - INTERVAL '7 days' + INTERVAL '1 hour', NOW() - INTERVAL '6 days'),
('F0000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900200000000000104' LIMIT 1),
 'data_spike', 'medium', 'false_positive',
 '{"expected_mb":100,"actual_mb":800,"ratio":8,"period":"6h","note":"Firmware guncelleme sonrasi normal"}',
 'anomaly_detector', NOW() - INTERVAL '12 days', NOW() - INTERVAL '12 days' + INTERVAL '2 hours', NOW() - INTERVAL '11 days'),
-- Bosphorus IoT anomalies
('F0000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002',
 (SELECT id FROM sims WHERE iccid='89900200000000001002' LIMIT 1),
 'auth_flood', 'critical', 'open',
 '{"attempts":500,"period":"5m","source_nas":"10.0.0.2","threshold":100}',
 'auth_monitor', NOW() - INTERVAL '5 hours', NULL, NULL),
('F0000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002',
 (SELECT id FROM sims WHERE iccid='89900100000000001005' LIMIT 1),
 'data_spike', 'high', 'acknowledged',
 '{"expected_mb":200,"actual_mb":3000,"ratio":15,"period":"2h","apn":"iot.city"}',
 'anomaly_detector', NOW() - INTERVAL '2 days', NOW() - INTERVAL '1 day', NULL),
('F0000000-0000-0000-0000-000000000013', '10000000-0000-0000-0000-000000000002', NULL,
 'nas_flood', 'medium', 'resolved',
 '{"requests_per_sec":5000,"source_nas":"10.0.0.2","normal_rate":200}',
 'traffic_monitor', NOW() - INTERVAL '15 days', NOW() - INTERVAL '15 days' + INTERVAL '30 minutes', NOW() - INTERVAL '14 days')
ON CONFLICT DO NOTHING;

-- ============================================================
-- NOTIFICATION CONFIGS
-- ============================================================
INSERT INTO notification_configs (id, tenant_id, user_id, event_type, scope_type, channels, threshold_type, threshold_value, enabled) VALUES
('CC000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'ip_pool_warning', 'system', '{"in_app":true,"email":true,"webhook":false}', 'percentage', 80.00, true),
('CC000000-0000-0000-0000-000000000002', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000001', 'anomaly_detected', 'system', '{"in_app":true,"email":true,"webhook":true}', NULL, NULL, true),
('CC000000-0000-0000-0000-000000000003', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000002', 'sim_state_change', 'system', '{"in_app":true,"email":false,"webhook":false}', NULL, NULL, true),
('CC000000-0000-0000-0000-000000000004', '10000000-0000-0000-0000-000000000001', '40000000-0000-0000-0000-000000000005', 'operator_degraded', 'system', '{"in_app":true,"email":true,"webhook":false}', NULL, NULL, true),
('CC000000-0000-0000-0000-000000000011', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'ip_pool_warning', 'system', '{"in_app":true,"email":true,"webhook":false}', 'percentage', 80.00, true),
('CC000000-0000-0000-0000-000000000012', '10000000-0000-0000-0000-000000000002', '40000000-0000-0000-0000-000000000011', 'anomaly_detected', 'system', '{"in_app":true,"email":true,"webhook":false}', NULL, NULL, true)
ON CONFLICT DO NOTHING;

-- ============================================================
-- AUDIT LOGS (200+ with hash chain)
-- ============================================================
DO $$
DECLARE
    prev_h TEXT := '0000000000000000000000000000000000000000000000000000000000000000';
    cur_h TEXT;
    i INT;
    t_id UUID;
    u_id UUID;
    act TEXT;
    ent_type TEXT;
    ent_id TEXT;
    ts TIMESTAMPTZ;
    tenants_arr UUID[] := ARRAY['10000000-0000-0000-0000-000000000001'::uuid, '10000000-0000-0000-0000-000000000002'::uuid];
    users_nar UUID[] := ARRAY['40000000-0000-0000-0000-000000000001'::uuid, '40000000-0000-0000-0000-000000000002'::uuid, '40000000-0000-0000-0000-000000000003'::uuid, '40000000-0000-0000-0000-000000000004'::uuid, '40000000-0000-0000-0000-000000000005'::uuid];
    users_bio UUID[] := ARRAY['40000000-0000-0000-0000-000000000011'::uuid, '40000000-0000-0000-0000-000000000012'::uuid, '40000000-0000-0000-0000-000000000013'::uuid, '40000000-0000-0000-0000-000000000014'::uuid];
    actions TEXT[] := ARRAY['sim.create', 'sim.activate', 'sim.suspend', 'sim.terminate', 'apn.create', 'apn.update', 'policy.create', 'policy.activate', 'policy.rollout', 'user.login', 'user.login_failed', 'api_key.create', 'job.create', 'job.complete', 'session.start', 'session.end', 'ip_pool.allocate', 'notification.send', 'operator.health_check', 'sim.bulk_import'];
    entity_types TEXT[] := ARRAY['sim', 'sim', 'sim', 'sim', 'apn', 'apn', 'policy', 'policy', 'policy_rollout', 'user', 'user', 'api_key', 'job', 'job', 'session', 'session', 'ip_address', 'notification', 'operator', 'job'];
BEGIN
    FOR i IN 1..250 LOOP
        IF i % 3 != 0 THEN
            t_id := tenants_arr[1];
            u_id := users_nar[1 + (random()*4)::int];
        ELSE
            t_id := tenants_arr[2];
            u_id := users_bio[1 + (random()*3)::int];
        END IF;

        act := actions[1 + (random()*19)::int];
        ent_type := entity_types[1 + (random()*19)::int];
        ent_id := gen_random_uuid()::text;
        ts := NOW() - ((250 - i) * INTERVAL '2 hours') + (random() * INTERVAL '1 hour');

        cur_h := encode(sha256((prev_h || '|' || t_id || '|' || u_id || '|' || act || '|' || ent_type || '|' || ent_id || '|' || ts::text)::bytea), 'hex');

        INSERT INTO audit_logs (tenant_id, user_id, action, entity_type, entity_id, before_data, after_data, ip_address, hash, prev_hash, created_at)
        VALUES (
            t_id,
            u_id,
            act,
            ent_type,
            ent_id,
            CASE WHEN act LIKE '%.update' OR act LIKE '%.suspend' OR act LIKE '%.terminate' THEN '{"state":"active"}'::jsonb ELSE NULL END,
            CASE WHEN act LIKE '%.create' THEN '{"state":"active"}'::jsonb
                 WHEN act LIKE '%.activate' THEN '{"state":"active"}'::jsonb
                 WHEN act LIKE '%.suspend' THEN '{"state":"suspended"}'::jsonb
                 WHEN act LIKE '%.terminate' THEN '{"state":"terminated"}'::jsonb
                 ELSE '{}'::jsonb END,
            ('192.168.1.' || (1 + (random()*254)::int))::inet,
            cur_h,
            prev_h,
            ts
        );

        prev_h := cur_h;
    END LOOP;
END $$;

-- ============================================================
-- SIM STATE HISTORY (for SIM detail history tab)
-- ============================================================
INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT
    s.id,
    NULL,
    'ordered',
    'Toplu aktarim ile olusturuldu',
    'system',
    NULL,
    s.created_at
FROM sims s
WHERE s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
  AND s.created_at >= '2026-03-01'
ON CONFLICT DO NOTHING;

INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT
    s.id,
    'ordered',
    'active',
    'Otomatik aktivasyon',
    'system',
    NULL,
    COALESCE(s.activated_at, s.created_at + INTERVAL '1 hour')
FROM sims s
WHERE s.state IN ('active', 'suspended', 'terminated', 'stolen_lost')
  AND s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
  AND COALESCE(s.activated_at, s.created_at + INTERVAL '1 hour') >= '2026-03-01'
ON CONFLICT DO NOTHING;

INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT
    s.id,
    'active',
    'suspended',
    'Manuel askiya alma',
    'user',
    '40000000-0000-0000-0000-000000000001',
    COALESCE(s.suspended_at, s.created_at + INTERVAL '30 days')
FROM sims s
WHERE s.state = 'suspended'
  AND s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
  AND COALESCE(s.suspended_at, s.created_at + INTERVAL '30 days') >= '2026-03-01'
ON CONFLICT DO NOTHING;

INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT
    s.id,
    'active',
    'terminated',
    'Sozlesme feshi',
    'user',
    '40000000-0000-0000-0000-000000000001',
    COALESCE(s.terminated_at, s.created_at + INTERVAL '60 days')
FROM sims s
WHERE s.state = 'terminated'
  AND s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
  AND COALESCE(s.terminated_at, s.created_at + INTERVAL '60 days') >= '2026-03-01'
ON CONFLICT DO NOTHING;

INSERT INTO sim_state_history (sim_id, from_state, to_state, reason, triggered_by, user_id, created_at)
SELECT
    s.id,
    'active',
    'stolen_lost',
    'Calinti bildirimi',
    'user',
    '40000000-0000-0000-0000-000000000001',
    s.created_at + INTERVAL '10 days'
FROM sims s
WHERE s.state = 'stolen_lost'
  AND s.tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002')
  AND s.created_at + INTERVAL '10 days' >= '2026-03-01'
ON CONFLICT DO NOTHING;

-- ============================================================
-- TENANT RETENTION CONFIG (only if table exists)
-- ============================================================
DO $$ BEGIN
IF EXISTS (SELECT 1 FROM pg_tables WHERE tablename='tenant_retention_config') THEN
    INSERT INTO tenant_retention_config (tenant_id, cdr_retention_days, session_retention_days, audit_retention_days, s3_archival_enabled) VALUES
    ('10000000-0000-0000-0000-000000000001', 365, 365, 730, false),
    ('10000000-0000-0000-0000-000000000002', 180, 180, 365, false)
    ON CONFLICT (tenant_id) DO NOTHING;
END IF;
END $$;

-- ============================================================
-- FIX TENANT USER PASSWORD HASHES
-- All seeded users: password = "password123" (bcrypt $2a$12)
-- ============================================================
UPDATE users SET password_hash = '$2a$12$ykM9KdOoZNshmojSwWvMpOiLhroGvbUpCKBG2nYSj73vjU1G8oCYK'
WHERE tenant_id IN ('10000000-0000-0000-0000-000000000001', '10000000-0000-0000-0000-000000000002');

-- ============================================================
-- ARGUS DEMO TENANT SEED DATA
-- Admin (admin@argus.io) is in '00000000-0000-0000-0000-000000000001'
-- Give this tenant real data so the dashboard shows content
-- ============================================================

-- Operator grants for Argus Demo
INSERT INTO operator_grants (id, tenant_id, operator_id, enabled) VALUES
('30000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', true),
('30000000-0000-0000-0000-000000000011', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', true),
('30000000-0000-0000-0000-000000000012', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', true)
ON CONFLICT DO NOTHING;

-- Demo tenant users
INSERT INTO users (id, tenant_id, email, password_hash, name, role, state, totp_enabled, last_login_at) VALUES
('00000000-0000-0000-0000-000000000011', '00000000-0000-0000-0000-000000000001', 'demo.admin@argus.io',
 '$2a$12$ykM9KdOoZNshmojSwWvMpOiLhroGvbUpCKBG2nYSj73vjU1G8oCYK', 'Demo Admin', 'tenant_admin', 'active', false, NOW() - INTERVAL '1 hour'),
('00000000-0000-0000-0000-000000000012', '00000000-0000-0000-0000-000000000001', 'demo.manager@argus.io',
 '$2a$12$ykM9KdOoZNshmojSwWvMpOiLhroGvbUpCKBG2nYSj73vjU1G8oCYK', 'Demo Manager', 'sim_manager', 'active', false, NOW() - INTERVAL '3 hours'),
('00000000-0000-0000-0000-000000000013', '00000000-0000-0000-0000-000000000001', 'demo.analyst@argus.io',
 '$2a$12$ykM9KdOoZNshmojSwWvMpOiLhroGvbUpCKBG2nYSj73vjU1G8oCYK', 'Demo Analyst', 'auditor', 'active', false, NOW() - INTERVAL '6 hours')
ON CONFLICT DO NOTHING;

-- Demo tenant policies
INSERT INTO policies (id, tenant_id, name, description, scope, state, created_by) VALUES
('05000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'Demo Standard QoS', 'Default QoS policy for demo tenant', 'apn', 'active', '00000000-0000-0000-0000-000000000010'),
('05000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'Demo IoT Savings', 'Low-bandwidth policy for demo IoT devices', 'global', 'active', '00000000-0000-0000-0000-000000000010'),
('05000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'Demo Premium', 'High priority policy for demo premium devices', 'apn', 'active', '00000000-0000-0000-0000-000000000010')
ON CONFLICT DO NOTHING;

-- Demo tenant policy versions
INSERT INTO policy_versions (id, policy_id, version, dsl_content, compiled_rules, state, affected_sim_count, activated_at, created_by) VALUES
('05100000-0000-0000-0000-000000000001', '05000000-0000-0000-0000-000000000001', 1,
 'POLICY "demo-std-v1" { MATCH { apn = "iot.demo" } RULES { bandwidth_down = 10mbps; bandwidth_up = 5mbps; priority = 5 } }',
 '{"name":"demo-std-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"iot.demo"}]},"rules":{"defaults":{"bandwidth_down":10000000,"bandwidth_up":5000000,"priority":5},"when_blocks":[]}}',
 'active', 20, NOW() - INTERVAL '30 days', '00000000-0000-0000-0000-000000000010'),
('05100000-0000-0000-0000-000000000002', '05000000-0000-0000-0000-000000000002', 1,
 'POLICY "demo-iot-v1" { MATCH { rat_type = "nb_iot" } RULES { bandwidth_down = 128kbps; bandwidth_up = 64kbps; max_daily_mb = 100 } }',
 '{"name":"demo-iot-v1","match":{"conditions":[{"field":"rat_type","op":"eq","value":"nb_iot"}]},"rules":{"defaults":{"bandwidth_down":128000,"bandwidth_up":64000,"max_daily_mb":100},"when_blocks":[]}}',
 'active', 15, NOW() - INTERVAL '25 days', '00000000-0000-0000-0000-000000000010'),
('05100000-0000-0000-0000-000000000003', '05000000-0000-0000-0000-000000000003', 1,
 'POLICY "demo-premium-v1" { MATCH { apn = "data.demo" } RULES { bandwidth_down = 50mbps; bandwidth_up = 20mbps; priority = 2 } }',
 '{"name":"demo-premium-v1","match":{"conditions":[{"field":"apn","op":"eq","value":"data.demo"}]},"rules":{"defaults":{"bandwidth_down":50000000,"bandwidth_up":20000000,"priority":2},"when_blocks":[]}}',
 'active', 10, NOW() - INTERVAL '20 days', '00000000-0000-0000-0000-000000000010')
ON CONFLICT DO NOTHING;

UPDATE policies SET current_version_id = '05100000-0000-0000-0000-000000000001' WHERE id = '05000000-0000-0000-0000-000000000001';
UPDATE policies SET current_version_id = '05100000-0000-0000-0000-000000000002' WHERE id = '05000000-0000-0000-0000-000000000002';
UPDATE policies SET current_version_id = '05100000-0000-0000-0000-000000000003' WHERE id = '05000000-0000-0000-0000-000000000003';

-- Demo tenant APNs
INSERT INTO apns (id, tenant_id, operator_id, name, display_name, apn_type, supported_rat_types, default_policy_id, state, created_by) VALUES
('06000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'iot.demo', 'Demo IoT', 'iot', ARRAY['lte','lte_m','nr_5g'], '05000000-0000-0000-0000-000000000001', 'active', '00000000-0000-0000-0000-000000000010'),
('06000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', 'm2m.demo', 'Demo M2M', 'm2m', ARRAY['nb_iot','lte_m'], '05000000-0000-0000-0000-000000000002', 'active', '00000000-0000-0000-0000-000000000010'),
('06000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'data.demo', 'Demo Data', 'iot', ARRAY['lte','nr_5g'], '05000000-0000-0000-0000-000000000003', 'active', '00000000-0000-0000-0000-000000000010'),
('06000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', 'sensor.demo', 'Demo Sensor', 'iot', ARRAY['nb_iot','lte_m','lte'], NULL, 'active', '00000000-0000-0000-0000-000000000010')
ON CONFLICT DO NOTHING;

-- Demo tenant IP pools
INSERT INTO ip_pools (id, tenant_id, apn_id, name, cidr_v4, cidr_v6, total_addresses, used_addresses, alert_threshold_warning, alert_threshold_critical, state) VALUES
('07000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', 'Demo IoT Pool', '10.20.0.0/22', 'fd20::/64', 1022, 312, 80, 90, 'active'),
('07000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', 'Demo M2M Pool', '10.21.0.0/24', NULL, 254, 198, 80, 90, 'active'),
('07000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000003', 'Demo Data Pool', '10.22.0.0/24', 'fd22::/64', 254, 87, 80, 90, 'active'),
('07000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000004', 'Demo Sensor Pool', '10.23.0.0/25', NULL, 126, 115, 80, 90, 'active')
ON CONFLICT DO NOTHING;

-- Demo tenant SIMs (50 total: varied states and operators)
-- Active SIMs on iot.demo (Turkcell)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at) VALUES
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002001', '286010000002001', '905303002001', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '55 days', NOW() - INTERVAL '60 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002002', '286010000002002', '905303002002', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002003', '286010000002003', '905303002003', 'physical', 'active', 'nr_5g', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002004', '286010000002004', '905303002004', 'esim', 'active', 'lte_m', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002005', '286010000002005', '905303002005', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002006', '286010000002006', '905303002006', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002007', '286010000002007', '905303002007', 'physical', 'active', 'nr_5g', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002008', '286010000002008', '905303002008', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '20 days', NOW() - INTERVAL '25 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002009', '286010000002009', '905303002009', 'physical', 'active', 'lte_m', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '15 days', NOW() - INTERVAL '20 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002010', '286010000002010', '905303002010', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000001', NOW() - INTERVAL '10 days', NOW() - INTERVAL '15 days'),
-- Active SIMs on m2m.demo (Turkcell, NB-IoT)
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002101', '286010000002101', '905303002101', 'physical', 'active', 'nb_iot', '05100000-0000-0000-0000-000000000002', NOW() - INTERVAL '50 days', NOW() - INTERVAL '55 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002102', '286010000002102', '905303002102', 'physical', 'active', 'nb_iot', '05100000-0000-0000-0000-000000000002', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002103', '286010000002103', '905303002103', 'physical', 'active', 'lte_m', '05100000-0000-0000-0000-000000000002', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002104', '286010000002104', '905303002104', 'physical', 'active', 'nb_iot', '05100000-0000-0000-0000-000000000002', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002105', '286010000002105', '905303002105', 'physical', 'active', 'nb_iot', '05100000-0000-0000-0000-000000000002', NOW() - INTERVAL '28 days', NOW() - INTERVAL '33 days'),
-- Active SIMs on data.demo (Vodafone)
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002001', '286020000002001', '905424002001', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000003', NOW() - INTERVAL '45 days', NOW() - INTERVAL '50 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002002', '286020000002002', '905424002002', 'physical', 'active', 'nr_5g', '05100000-0000-0000-0000-000000000003', NOW() - INTERVAL '40 days', NOW() - INTERVAL '45 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002003', '286020000002003', '905424002003', 'esim', 'active', 'lte', '05100000-0000-0000-0000-000000000003', NOW() - INTERVAL '35 days', NOW() - INTERVAL '40 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002004', '286020000002004', '905424002004', 'physical', 'active', 'lte', '05100000-0000-0000-0000-000000000003', NOW() - INTERVAL '30 days', NOW() - INTERVAL '35 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002005', '286020000002005', '905424002005', 'physical', 'active', 'nr_5g', '05100000-0000-0000-0000-000000000003', NOW() - INTERVAL '25 days', NOW() - INTERVAL '30 days'),
-- Active SIMs on sensor.demo (Vodafone)
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000004', '89900200000000002101', '286020000002101', '905424002101', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '42 days', NOW() - INTERVAL '47 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000004', '89900200000000002102', '286020000002102', '905424002102', 'physical', 'active', 'lte_m', NULL, NOW() - INTERVAL '38 days', NOW() - INTERVAL '43 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000004', '89900200000000002103', '286020000002103', '905424002103', 'physical', 'active', 'nb_iot', NULL, NOW() - INTERVAL '32 days', NOW() - INTERVAL '37 days'),
-- Turk Telekom SIMs
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '06000000-0000-0000-0000-000000000001', '89900300000000002001', '286030000002001', '905552002001', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '38 days', NOW() - INTERVAL '43 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '06000000-0000-0000-0000-000000000001', '89900300000000002002', '286030000002002', '905552002002', 'physical', 'active', 'nr_5g', NULL, NOW() - INTERVAL '33 days', NOW() - INTERVAL '38 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000003', '06000000-0000-0000-0000-000000000001', '89900300000000002003', '286030000002003', '905552002003', 'physical', 'active', 'lte', NULL, NOW() - INTERVAL '28 days', NOW() - INTERVAL '33 days'),
-- Suspended SIMs
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002301', '286010000002301', '905303002301', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '80 days', NOW() - INTERVAL '85 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002302', '286010000002302', '905303002302', 'physical', 'suspended', 'nb_iot', NULL, NOW() - INTERVAL '70 days', NOW() - INTERVAL '75 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002301', '286020000002301', '905424002301', 'physical', 'suspended', 'lte', NULL, NOW() - INTERVAL '60 days', NOW() - INTERVAL '65 days'),
-- Ordered SIMs
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002401', '286010000002401', '905303002401', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '4 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000002', '89900100000000002402', '286010000002402', '905303002402', 'physical', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '2 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', NULL, '89900200000000002401', '286020000002401', '905424002401', 'esim', 'ordered', NULL, NULL, NULL, NOW() - INTERVAL '1 day'),
-- Terminated SIMs
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002501', '286010000002501', '905303002501', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '110 days', NOW() - INTERVAL '115 days'),
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000002', '06000000-0000-0000-0000-000000000003', '89900200000000002501', '286020000002501', '905424002501', 'physical', 'terminated', 'lte', NULL, NOW() - INTERVAL '95 days', NOW() - INTERVAL '100 days'),
-- Stolen/lost
(gen_random_uuid(), '00000000-0000-0000-0000-000000000001', '20000000-0000-0000-0000-000000000001', '06000000-0000-0000-0000-000000000001', '89900100000000002601', '286010000002601', '905303002601', 'physical', 'stolen_lost', 'lte', NULL, NOW() - INTERVAL '25 days', NOW() - INTERVAL '50 days')
ON CONFLICT DO NOTHING;

-- Additional bulk SIMs for demo tenant (20 more)
INSERT INTO sims (id, tenant_id, operator_id, apn_id, iccid, imsi, msisdn, sim_type, state, rat_type, policy_version_id, activated_at, created_at)
SELECT
    gen_random_uuid(),
    '00000000-0000-0000-0000-000000000001',
    CASE WHEN i % 3 = 0 THEN '20000000-0000-0000-0000-000000000003'::uuid
         WHEN i % 3 = 1 THEN '20000000-0000-0000-0000-000000000001'::uuid
         ELSE '20000000-0000-0000-0000-000000000002'::uuid END,
    CASE WHEN i % 2 = 0 THEN '06000000-0000-0000-0000-000000000001'::uuid ELSE '06000000-0000-0000-0000-000000000002'::uuid END,
    '89900100000000007' || LPAD(i::text, 3, '0'),
    '28601000000' || LPAD((7000+i)::text, 4, '0'),
    '9053030007' || LPAD(i::text, 2, '0'),
    'physical',
    'active',
    CASE WHEN i % 3 = 0 THEN 'lte' WHEN i % 3 = 1 THEN 'nb_iot' ELSE 'lte_m' END,
    CASE WHEN i % 2 = 0 THEN '05100000-0000-0000-0000-000000000001'::uuid ELSE '05100000-0000-0000-0000-000000000002'::uuid END,
    NOW() - (i * INTERVAL '1 day'),
    NOW() - ((i+5) * INTERVAL '1 day')
FROM generate_series(1, 20) AS s(i)
ON CONFLICT DO NOTHING;

-- Active sessions for Demo tenant SIMs
INSERT INTO sessions (id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip, calling_station_id, called_station_id, rat_type, session_state, auth_method, protocol_type, acct_session_id, started_at, bytes_in, bytes_out, packets_in, packets_out, last_interim_at)
SELECT
    gen_random_uuid(),
    s.id,
    s.tenant_id,
    s.operator_id,
    s.apn_id,
    '10.0.0.5'::inet,
    ('10.20.' || ((row_number() OVER()) / 256) || '.' || ((row_number() OVER()) % 256))::inet,
    s.msisdn,
    a.name,
    s.rat_type,
    'active',
    'eap_sim',
    CASE WHEN s.rat_type = 'nr_5g' THEN '5g_sba' WHEN s.rat_type IN ('nb_iot','lte_m') THEN 'diameter' ELSE 'radius' END,
    'ACCT-DEM-' || LPAD((row_number() OVER())::text, 6, '0'),
    NOW() - (random() * INTERVAL '10 hours'),
    (random() * 80000000)::bigint,
    (random() * 40000000)::bigint,
    (random() * 80000)::bigint,
    (random() * 40000)::bigint,
    NOW() - (random() * INTERVAL '25 minutes')
FROM sims s
JOIN apns a ON s.apn_id = a.id
WHERE s.tenant_id = '00000000-0000-0000-0000-000000000001'
  AND s.state = 'active'
  AND s.apn_id IS NOT NULL
LIMIT 25;

-- Historical sessions for Demo tenant
INSERT INTO sessions (id, sim_id, tenant_id, operator_id, apn_id, nas_ip, framed_ip, calling_station_id, rat_type, session_state, auth_method, protocol_type, acct_session_id, started_at, ended_at, terminate_cause, bytes_in, bytes_out, packets_in, packets_out)
SELECT
    gen_random_uuid(),
    s.id,
    s.tenant_id,
    s.operator_id,
    s.apn_id,
    '10.0.0.5'::inet,
    ('10.20.' || ((i*3 + row_number() OVER()) / 256) || '.' || ((i*3 + row_number() OVER()) % 256))::inet,
    s.msisdn,
    s.rat_type,
    'closed',
    'eap_sim',
    'radius',
    'HIST-DEM-' || LPAD(i::text, 3, '0') || '-' || LPAD((row_number() OVER())::text, 4, '0'),
    NOW() - (i * INTERVAL '1 day') - (random() * INTERVAL '12 hours'),
    NOW() - (i * INTERVAL '1 day') - (random() * INTERVAL '12 hours') + (random() * INTERVAL '4 hours'),
    (ARRAY['User-Request','Lost-Carrier','Idle-Timeout','Session-Timeout','Admin-Reset'])[1 + (random()*4)::int],
    (random() * 150000000)::bigint,
    (random() * 75000000)::bigint,
    (random() * 150000)::bigint,
    (random() * 75000)::bigint
FROM sims s
CROSS JOIN generate_series(1, 7) AS gs(i)
WHERE s.tenant_id = '00000000-0000-0000-0000-000000000001'
  AND s.state = 'active'
  AND s.apn_id IS NOT NULL
LIMIT 150;

-- CDRs for Demo tenant (30-day chart data)
INSERT INTO cdrs (session_id, sim_id, tenant_id, operator_id, apn_id, rat_type, record_type, bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp)
SELECT
    sess.id,
    sess.sim_id,
    sess.tenant_id,
    sess.operator_id,
    sess.apn_id,
    sess.rat_type,
    'stop',
    (random() * 60000000)::bigint,
    (random() * 30000000)::bigint,
    (random() * 5400)::int,
    round((random() * 6.0)::numeric, 4),
    round((random() * 4.0)::numeric, 4),
    round((random() * 0.12)::numeric, 4),
    CASE
        WHEN sess.rat_type = 'nr_5g' THEN 1.5
        WHEN sess.rat_type = 'lte' THEN 1.0
        WHEN sess.rat_type = 'lte_m' THEN 0.8
        WHEN sess.rat_type = 'nb_iot' THEN 0.5
        ELSE 1.0
    END,
    NOW() - ((d * 24 + (random()*23)::int) * INTERVAL '1 hour')
FROM generate_series(0, 29) AS gs(d)
CROSS JOIN LATERAL (
    SELECT id, sim_id, tenant_id, operator_id, apn_id, rat_type
    FROM sessions
    WHERE tenant_id = '00000000-0000-0000-0000-000000000001'
    ORDER BY random()
    LIMIT 5
) sess
ON CONFLICT DO NOTHING;

-- Jobs for Demo tenant
INSERT INTO jobs (id, tenant_id, type, state, priority, payload, total_items, processed_items, failed_items, progress_pct, started_at, completed_at, created_at, created_by) VALUES
('A1000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'completed', 5,
 '{"filename":"demo_iot_batch1.csv","operator":"Turkcell","apn":"iot.demo"}', 35, 35, 0, 100.00,
 NOW() - INTERVAL '25 days', NOW() - INTERVAL '25 days' + INTERVAL '4 minutes', NOW() - INTERVAL '25 days', '00000000-0000-0000-0000-000000000010'),
('A1000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'completed', 5,
 '{"filename":"demo_m2m_batch.csv","operator":"Turkcell","apn":"m2m.demo"}', 20, 19, 1, 100.00,
 NOW() - INTERVAL '18 days', NOW() - INTERVAL '18 days' + INTERVAL '2 minutes', NOW() - INTERVAL '18 days', '00000000-0000-0000-0000-000000000011'),
('A1000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'policy_rollout', 'completed', 7,
 '{"policy":"Demo Standard QoS","version":1,"strategy":"immediate"}', 20, 20, 0, 100.00,
 NOW() - INTERVAL '28 days', NOW() - INTERVAL '28 days' + INTERVAL '60 seconds', NOW() - INTERVAL '28 days', '00000000-0000-0000-0000-000000000010'),
('A1000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', 'sim_state_change', 'completed', 3,
 '{"action":"suspend","reason":"Trial period ended","sim_count":3}', 3, 3, 0, 100.00,
 NOW() - INTERVAL '12 days', NOW() - INTERVAL '12 days' + INTERVAL '20 seconds', NOW() - INTERVAL '12 days', '00000000-0000-0000-0000-000000000010'),
('A1000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', 'cdr_export', 'running', 4,
 '{"format":"csv","date_range":"2026-03-01 to 2026-03-23"}', 800, 560, 0, 70.00,
 NOW() - INTERVAL '30 minutes', NULL, NOW() - INTERVAL '35 minutes', '00000000-0000-0000-0000-000000000013'),
('A1000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', 'ip_pool_reclaim', 'completed', 2,
 '{"pool":"Demo Sensor Pool","reclaimed":8}', 8, 8, 0, 100.00,
 NOW() - INTERVAL '5 days', NOW() - INTERVAL '5 days' + INTERVAL '45 seconds', NOW() - INTERVAL '5 days', NULL),
('A1000000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', 'sim_bulk_import', 'queued', 5,
 '{"filename":"demo_vodafone_batch.csv","operator":"Vodafone TR","apn":"data.demo"}', 15, 0, 0, 0.00,
 NULL, NULL, NOW() - INTERVAL '10 minutes', '00000000-0000-0000-0000-000000000011')
ON CONFLICT DO NOTHING;

-- Notifications for Demo tenant
INSERT INTO notifications (id, tenant_id, user_id, event_type, scope_type, title, body, severity, channels_sent, state, read_at, created_at) VALUES
('BB000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'ip_pool_warning', 'apn', 'IP Pool Warning: Demo Sensor Pool', 'Demo Sensor Pool is at 91% capacity. Expansion needed.', 'critical', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '3 hours'),
('BB000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'ip_pool_warning', 'apn', 'IP Pool Warning: Demo M2M Pool', 'Demo M2M Pool is at 78% capacity.', 'warning', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '5 hours'),
('BB000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000011', 'job_completed', 'system', 'Bulk Import Completed', 'demo_iot_batch1.csv: 35/35 successful.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '24 hours', NOW() - INTERVAL '25 days'),
('BB000000-0000-0000-0000-000000000004', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'operator_degraded', 'operator', 'Operator Status: Turk Telekom DEGRADED', 'Turk Telekom health check failed. Circuit breaker engaged.', 'warning', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '7 hours'),
('BB000000-0000-0000-0000-000000000005', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000013', 'anomaly_detected', 'sim', 'Anomaly Detected: Data Spike', 'SIM 89900100000000002003 used 30x normal data in 1 hour.', 'critical', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '2 hours'),
('BB000000-0000-0000-0000-000000000006', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'policy_activated', 'policy', 'Policy Activated', 'Demo Standard QoS v1 successfully activated.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '27 days', NOW() - INTERVAL '28 days'),
('BB000000-0000-0000-0000-000000000007', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'sim_state_change', 'sim', 'SIM Status Changed', '3 SIMs moved to SUSPENDED state.', 'info', ARRAY['in_app'], 'read', NOW() - INTERVAL '11 days', NOW() - INTERVAL '12 days'),
('BB000000-0000-0000-0000-000000000008', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000011', 'sim_stolen_lost', 'sim', 'SIM Stolen/Lost Report', 'SIM 89900100000000002601 marked as stolen. Session terminated.', 'critical', ARRAY['in_app','email'], 'read', NOW() - INTERVAL '24 days', NOW() - INTERVAL '25 days'),
('BB000000-0000-0000-0000-000000000009', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000013', 'api_key_expiring', 'system', 'API Key Expiring Soon', 'Demo Analytics API key expires in 7 days.', 'warning', ARRAY['in_app','email'], 'unread', NULL, NOW() - INTERVAL '1 day'),
('BB000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'job_completed', 'system', 'CDR Export Ready', 'CDR export for March 2026 is ready for download.', 'info', ARRAY['in_app'], 'unread', NULL, NOW() - INTERVAL '6 hours')
ON CONFLICT DO NOTHING;

-- API Keys for Demo tenant
INSERT INTO api_keys (id, tenant_id, name, key_prefix, key_hash, scopes, rate_limit_per_minute, rate_limit_per_hour, expires_at, last_used_at, usage_count, created_by) VALUES
('CC100000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', 'Demo Fleet API', 'arg_df01', '$2a$12$demohash1forfleetapikey0000000000000000000000000000', '["sims:read","sims:write","sessions:read"]', 300, 9000, NOW() + INTERVAL '60 days', NOW() - INTERVAL '2 hours', 5430, '00000000-0000-0000-0000-000000000010'),
('CC100000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', 'Demo Analytics API', 'arg_da01', '$2a$12$demohash2foranalyticskey000000000000000000000000000', '["cdrs:read","analytics:read","sessions:read"]', 500, 15000, NOW() + INTERVAL '30 days', NOW() - INTERVAL '4 hours', 2100, '00000000-0000-0000-0000-000000000010'),
('CC100000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', 'Demo Webhook', 'arg_dw01', '$2a$12$demohash3forwebhookkey0000000000000000000000000000', '["notifications:read","events:subscribe"]', 100, 3000, NOW() + INTERVAL '90 days', NOW() - INTERVAL '8 hours', 890, '00000000-0000-0000-0000-000000000011')
ON CONFLICT DO NOTHING;

-- Anomalies for Demo tenant
INSERT INTO anomalies (id, tenant_id, sim_id, type, severity, state, details, source, detected_at, acknowledged_at, resolved_at) VALUES
('FF000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000002003' LIMIT 1),
 'data_spike', 'critical', 'open',
 '{"expected_mb":30,"actual_mb":900,"ratio":30,"period":"1h","apn":"iot.demo"}',
 'anomaly_detector', NOW() - INTERVAL '2 hours', NULL, NULL),
('FF000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001',
 (SELECT id FROM sims WHERE iccid='89900100000000002301' LIMIT 1),
 'sim_cloning', 'high', 'acknowledged',
 '{"concurrent_sessions":2,"different_nas":["10.0.0.1","10.0.0.5"],"evidence":"2 concurrent sessions from different NAS"}',
 'session_monitor', NOW() - INTERVAL '4 days', NOW() - INTERVAL '3 days', NULL),
('FF000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', NULL,
 'auth_flood', 'medium', 'resolved',
 '{"attempts":800,"period":"5m","source_nas":"10.0.0.5","threshold":100}',
 'auth_monitor', NOW() - INTERVAL '10 days', NOW() - INTERVAL '10 days' + INTERVAL '30 minutes', NOW() - INTERVAL '9 days')
ON CONFLICT DO NOTHING;

-- Policy assignments for Demo tenant SIMs
INSERT INTO policy_assignments (id, sim_id, policy_version_id, rollout_id, assigned_at, coa_status)
SELECT
    gen_random_uuid(),
    s.id,
    s.policy_version_id,
    NULL,
    s.activated_at + INTERVAL '1 hour',
    'acked'
FROM sims s
WHERE s.policy_version_id IS NOT NULL
  AND s.tenant_id = '00000000-0000-0000-0000-000000000001'
ON CONFLICT DO NOTHING;

-- Notification configs for Demo tenant
INSERT INTO notification_configs (id, tenant_id, user_id, event_type, scope_type, channels, threshold_type, threshold_value, enabled) VALUES
('CD000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'ip_pool_warning', 'system', '{"in_app":true,"email":true,"webhook":false}', 'percentage', 80.00, true),
('CD000000-0000-0000-0000-000000000002', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000010', 'anomaly_detected', 'system', '{"in_app":true,"email":true,"webhook":false}', NULL, NULL, true),
('CD000000-0000-0000-0000-000000000003', '00000000-0000-0000-0000-000000000001', '00000000-0000-0000-0000-000000000011', 'sim_state_change', 'system', '{"in_app":true,"email":false,"webhook":false}', NULL, NULL, true)
ON CONFLICT DO NOTHING;

COMMIT;
