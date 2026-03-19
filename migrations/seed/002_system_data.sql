-- SEED-02: System Initial Data
-- Idempotent: all entries use ON CONFLICT DO NOTHING

-- Mock operator (for development/testing)
INSERT INTO operators (id, name, code, mcc, mnc, adapter_type, adapter_config, supported_rat_types, health_status, state)
VALUES (
    '00000000-0000-0000-0000-000000000100',
    'Mock Simulator',
    'mock',
    '999',
    '99',
    'mock',
    '{"host": "localhost", "port": 1812}',
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
