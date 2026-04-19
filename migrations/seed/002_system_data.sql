-- SEED-02: System Initial Data
-- Idempotent: all entries use ON CONFLICT DO NOTHING

-- Mock operator (for development/testing)
-- STORY-090 Wave 2 D2-B: adapter_type column dropped; adapter_config
-- carries the nested per-protocol enablement flags.
-- STORY-089 (2026-04-18): the mock operator does NOT enable the http sub-key.
-- No simulator path emulates 'mock'; all http routing goes through the three real operators.
INSERT INTO operators (id, name, code, mcc, mnc, adapter_config, supported_rat_types, health_status, state)
VALUES (
    '00000000-0000-0000-0000-000000000100',
    'Mock Simulator',
    'mock',
    '999',
    '99',
    '{"mock":{"enabled":true,"latency_ms":5,"simulated_imsi_count":1000}}',
    ARRAY['nb_iot', 'lte_m', 'lte', 'nr_5g'],
    'healthy',
    'active'
) ON CONFLICT (code) DO NOTHING;

-- Grant mock operator to demo tenant
INSERT INTO operator_grants (id, tenant_id, operator_id, enabled)
VALUES (
    '00000000-0000-0000-0000-000000000200',
    '00000000-0000-0000-0000-000000000001',
    '00000000-0000-0000-0000-000000000100',
    true
) ON CONFLICT DO NOTHING;

-- Create SIM partition for mock operator
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_class WHERE relname = 'sims_mock'
    ) THEN
        EXECUTE format(
            'CREATE TABLE sims_mock PARTITION OF sims FOR VALUES IN (%L)',
            '00000000-0000-0000-0000-000000000100'
        );
    END IF;
END
$$;
