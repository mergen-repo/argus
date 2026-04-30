-- FIX-106: Convert legacy flat adapter_config rows to nested shape.
-- Idempotent: only touches rows whose top-level JSON is NOT already
-- keyed by a known protocol name. Skips encrypted configs (first
-- byte is '"' not '{').
--
-- Also fixes the Mock Simulator row whose nested config has stray
-- RADIUS fields (host, port) instead of MockConfig fields.

DO $$
DECLARE
    r RECORD;
    raw JSONB;
    top_keys TEXT[];
    protocol TEXT;
    wrapped JSONB;
BEGIN
    FOR r IN SELECT id, adapter_config FROM operators
    LOOP
        -- Skip encrypted configs (stored as JSON string, starts with '"')
        IF left(r.adapter_config::text, 1) != '{' THEN
            CONTINUE;
        END IF;

        raw := r.adapter_config;

        -- Collect top-level keys
        SELECT array_agg(k) INTO top_keys FROM jsonb_object_keys(raw) AS k;

        IF top_keys IS NULL THEN
            CONTINUE;
        END IF;

        -- Check if already nested: ALL top-level keys are protocol names
        IF top_keys <@ ARRAY['radius','diameter','sba','http','mock'] THEN
            -- Already nested. But check for the Mock Simulator edge case:
            -- nested mock sub-object with stray host/port fields instead
            -- of proper MockConfig fields.
            IF raw ? 'mock' AND jsonb_typeof(raw->'mock') = 'object' THEN
                IF (raw->'mock') ? 'host' AND NOT (raw->'mock') ? 'latency_ms' THEN
                    UPDATE operators
                    SET adapter_config = jsonb_set(
                        raw,
                        '{mock}',
                        jsonb_build_object(
                            'enabled', COALESCE((raw->'mock'->>'enabled')::boolean, true),
                            'latency_ms', 5,
                            'simulated_imsi_count', 1000
                        )
                    )
                    WHERE id = r.id;
                END IF;
            END IF;
            CONTINUE;
        END IF;

        -- Flat config detected. Determine protocol via heuristic keys.
        protocol := NULL;

        IF raw ? 'shared_secret' OR raw ? 'listen_addr' OR raw ? 'acct_port' THEN
            protocol := 'radius';
        ELSIF raw ? 'origin_host' OR raw ? 'origin_realm' OR raw ? 'peers' OR raw ? 'product_name' THEN
            protocol := 'diameter';
        ELSIF raw ? 'nrf_url' OR raw ? 'nf_instance_id' THEN
            protocol := 'sba';
        ELSIF raw ? 'base_url' OR raw ? 'auth_type' OR raw ? 'auth_token' THEN
            protocol := 'http';
        ELSIF raw ? 'latency_ms' OR raw ? 'simulated_imsi_count' OR raw ? 'fail_rate'
              OR raw ? 'success_rate' OR raw ? 'healthy_after' OR raw ? 'error_type'
              OR raw ? 'timeout_ms' THEN
            protocol := 'mock';
        ELSE
            -- Unknown flat shape: wrap as mock (safe fallback for pre-090 catch-all)
            protocol := 'mock';
        END IF;

        -- Wrap: {"<protocol>": {"enabled": true, ...originalFields}}
        wrapped := jsonb_build_object(
            protocol,
            raw || '{"enabled": true}'::jsonb
        );

        UPDATE operators
        SET adapter_config = wrapped
        WHERE id = r.id;
    END LOOP;
END
$$;
