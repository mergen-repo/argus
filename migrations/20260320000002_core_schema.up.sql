-- Core Database Schema: All tables TBL-01 through TBL-25
-- Tables are created in dependency order

-- ============================================================
-- TBL-01: tenants
-- ============================================================
CREATE TABLE IF NOT EXISTS tenants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    domain VARCHAR(255) UNIQUE,
    contact_email VARCHAR(255) NOT NULL,
    contact_phone VARCHAR(50),
    max_sims INTEGER NOT NULL DEFAULT 100000,
    max_apns INTEGER NOT NULL DEFAULT 100,
    max_users INTEGER NOT NULL DEFAULT 50,
    purge_retention_days INTEGER NOT NULL DEFAULT 90,
    settings JSONB NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID,
    updated_by UUID
);

CREATE INDEX IF NOT EXISTS idx_tenants_domain ON tenants (domain);
CREATE INDEX IF NOT EXISTS idx_tenants_state ON tenants (state);

-- ============================================================
-- TBL-02: users
-- ============================================================
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(100) NOT NULL,
    role VARCHAR(30) NOT NULL,
    totp_secret VARCHAR(255),
    totp_enabled BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMPTZ,
    failed_login_count INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_tenant_email ON users (tenant_id, email);
CREATE INDEX IF NOT EXISTS idx_users_tenant_role ON users (tenant_id, role);
CREATE INDEX IF NOT EXISTS idx_users_state ON users (state);

-- ============================================================
-- TBL-03: user_sessions
-- ============================================================
CREATE TABLE IF NOT EXISTS user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    refresh_token_hash VARCHAR(255) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user ON user_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_expires ON user_sessions (expires_at) WHERE revoked_at IS NULL;

-- ============================================================
-- TBL-04: api_keys
-- ============================================================
CREATE TABLE IF NOT EXISTS api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    key_prefix VARCHAR(8) NOT NULL,
    key_hash VARCHAR(255) NOT NULL,
    scopes JSONB NOT NULL DEFAULT '["*"]',
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 1000,
    rate_limit_per_hour INTEGER NOT NULL DEFAULT 30000,
    expires_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    usage_count BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys (tenant_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys (key_prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_active ON api_keys (tenant_id) WHERE revoked_at IS NULL;

-- ============================================================
-- TBL-05: operators
-- ============================================================
CREATE TABLE IF NOT EXISTS operators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL UNIQUE,
    code VARCHAR(20) NOT NULL UNIQUE,
    mcc VARCHAR(3) NOT NULL,
    mnc VARCHAR(3) NOT NULL,
    adapter_type VARCHAR(30) NOT NULL,
    adapter_config JSONB NOT NULL DEFAULT '{}',
    sm_dp_plus_url VARCHAR(500),
    sm_dp_plus_config JSONB DEFAULT '{}',
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    health_status VARCHAR(20) NOT NULL DEFAULT 'unknown',
    health_check_interval_sec INTEGER NOT NULL DEFAULT 30,
    failover_policy VARCHAR(20) NOT NULL DEFAULT 'reject',
    failover_timeout_ms INTEGER NOT NULL DEFAULT 5000,
    circuit_breaker_threshold INTEGER NOT NULL DEFAULT 5,
    circuit_breaker_recovery_sec INTEGER NOT NULL DEFAULT 60,
    sla_uptime_target DECIMAL(5,2) DEFAULT 99.90,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_operators_code ON operators (code);
CREATE UNIQUE INDEX IF NOT EXISTS idx_operators_mcc_mnc ON operators (mcc, mnc);
CREATE INDEX IF NOT EXISTS idx_operators_state ON operators (state);

-- ============================================================
-- TBL-06: operator_grants
-- ============================================================
CREATE TABLE IF NOT EXISTS operator_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_operator_grants_tenant_op ON operator_grants (tenant_id, operator_id);
CREATE INDEX IF NOT EXISTS idx_operator_grants_tenant ON operator_grants (tenant_id) WHERE enabled = true;

-- ============================================================
-- TBL-13: policies (created before apns due to apns.default_policy_id reference)
-- ============================================================
CREATE TABLE IF NOT EXISTS policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    scope VARCHAR(20) NOT NULL,
    scope_ref_id UUID,
    current_version_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_policies_tenant_name ON policies (tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_policies_tenant_scope ON policies (tenant_id, scope);
CREATE INDEX IF NOT EXISTS idx_policies_state ON policies (state);

-- ============================================================
-- TBL-14: policy_versions
-- ============================================================
CREATE TABLE IF NOT EXISTS policy_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES policies(id),
    version INTEGER NOT NULL,
    dsl_content TEXT NOT NULL,
    compiled_rules JSONB NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'draft',
    affected_sim_count INTEGER,
    dry_run_result JSONB,
    activated_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_versions_policy_ver ON policy_versions (policy_id, version);
CREATE INDEX IF NOT EXISTS idx_policy_versions_policy_state ON policy_versions (policy_id, state);

-- Add FK from policies.current_version_id to policy_versions.id
ALTER TABLE policies ADD CONSTRAINT fk_policies_current_version
    FOREIGN KEY (current_version_id) REFERENCES policy_versions(id);

-- ============================================================
-- TBL-07: apns
-- ============================================================
CREATE TABLE IF NOT EXISTS apns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    name VARCHAR(100) NOT NULL,
    display_name VARCHAR(255),
    apn_type VARCHAR(20) NOT NULL,
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    default_policy_id UUID REFERENCES policies(id),
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    updated_by UUID REFERENCES users(id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_apns_tenant_name ON apns (tenant_id, operator_id, name);
CREATE INDEX IF NOT EXISTS idx_apns_tenant_state ON apns (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_apns_operator ON apns (operator_id);

-- ============================================================
-- TBL-08: ip_pools
-- ============================================================
CREATE TABLE IF NOT EXISTS ip_pools (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    apn_id UUID NOT NULL REFERENCES apns(id),
    name VARCHAR(100) NOT NULL,
    cidr_v4 CIDR,
    cidr_v6 CIDR,
    total_addresses INTEGER NOT NULL DEFAULT 0,
    used_addresses INTEGER NOT NULL DEFAULT 0,
    alert_threshold_warning INTEGER NOT NULL DEFAULT 80,
    alert_threshold_critical INTEGER NOT NULL DEFAULT 90,
    reclaim_grace_period_days INTEGER NOT NULL DEFAULT 7,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ip_pools_tenant_apn ON ip_pools (tenant_id, apn_id);
CREATE INDEX IF NOT EXISTS idx_ip_pools_apn ON ip_pools (apn_id);

-- ============================================================
-- TBL-09: ip_addresses
-- ============================================================
CREATE TABLE IF NOT EXISTS ip_addresses (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pool_id UUID NOT NULL REFERENCES ip_pools(id),
    address_v4 INET,
    address_v6 INET,
    allocation_type VARCHAR(10) NOT NULL DEFAULT 'dynamic',
    sim_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    allocated_at TIMESTAMPTZ,
    reclaim_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_ip_addresses_pool_state ON ip_addresses (pool_id, state);
CREATE INDEX IF NOT EXISTS idx_ip_addresses_sim ON ip_addresses (sim_id) WHERE sim_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ip_addresses_v4 ON ip_addresses (pool_id, address_v4) WHERE address_v4 IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_ip_addresses_v6 ON ip_addresses (pool_id, address_v6) WHERE address_v6 IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_ip_addresses_reclaim ON ip_addresses (reclaim_at) WHERE state = 'reclaiming';

-- ============================================================
-- TBL-12: esim_profiles (created before sims for FK reference)
-- ============================================================
CREATE TABLE IF NOT EXISTS esim_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL UNIQUE,
    eid VARCHAR(32) NOT NULL,
    sm_dp_plus_id VARCHAR(255),
    operator_id UUID NOT NULL REFERENCES operators(id),
    profile_state VARCHAR(20) NOT NULL DEFAULT 'disabled',
    iccid_on_profile VARCHAR(22),
    last_provisioned_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_esim_profiles_sim ON esim_profiles (sim_id);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_eid ON esim_profiles (eid);
CREATE INDEX IF NOT EXISTS idx_esim_profiles_operator ON esim_profiles (operator_id);

-- ============================================================
-- TBL-10: sims (partitioned by operator_id)
-- ============================================================
CREATE TABLE IF NOT EXISTS sims (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    iccid VARCHAR(22) NOT NULL,
    imsi VARCHAR(15) NOT NULL,
    msisdn VARCHAR(20),
    ip_address_id UUID,
    policy_version_id UUID,
    esim_profile_id UUID,
    sim_type VARCHAR(10) NOT NULL DEFAULT 'physical',
    state VARCHAR(20) NOT NULL DEFAULT 'ordered',
    rat_type VARCHAR(10),
    max_concurrent_sessions INTEGER NOT NULL DEFAULT 1,
    session_idle_timeout_sec INTEGER NOT NULL DEFAULT 3600,
    session_hard_timeout_sec INTEGER NOT NULL DEFAULT 86400,
    metadata JSONB NOT NULL DEFAULT '{}',
    activated_at TIMESTAMPTZ,
    suspended_at TIMESTAMPTZ,
    terminated_at TIMESTAMPTZ,
    purge_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, operator_id)
) PARTITION BY LIST (operator_id);

-- Create a default partition to catch any operator_id not explicitly listed
CREATE TABLE IF NOT EXISTS sims_default PARTITION OF sims DEFAULT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_sims_iccid ON sims (iccid, operator_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sims_imsi ON sims (imsi, operator_id);
CREATE INDEX IF NOT EXISTS idx_sims_msisdn ON sims (msisdn) WHERE msisdn IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sims_tenant_state ON sims (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_sims_tenant_operator ON sims (tenant_id, operator_id);
CREATE INDEX IF NOT EXISTS idx_sims_tenant_apn ON sims (tenant_id, apn_id);
CREATE INDEX IF NOT EXISTS idx_sims_tenant_policy ON sims (tenant_id, policy_version_id);
CREATE INDEX IF NOT EXISTS idx_sims_purge ON sims (purge_at) WHERE state = 'terminated';

-- ============================================================
-- TBL-11: sim_state_history (partitioned by created_at, monthly)
-- ============================================================
CREATE TABLE IF NOT EXISTS sim_state_history (
    id BIGSERIAL,
    sim_id UUID NOT NULL,
    from_state VARCHAR(20),
    to_state VARCHAR(20) NOT NULL,
    reason VARCHAR(255),
    triggered_by VARCHAR(20) NOT NULL,
    user_id UUID,
    job_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Create monthly partitions for current and upcoming months
CREATE TABLE IF NOT EXISTS sim_state_history_2026_03 PARTITION OF sim_state_history
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE IF NOT EXISTS sim_state_history_2026_04 PARTITION OF sim_state_history
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE IF NOT EXISTS sim_state_history_2026_05 PARTITION OF sim_state_history
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE IF NOT EXISTS sim_state_history_2026_06 PARTITION OF sim_state_history
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

CREATE INDEX IF NOT EXISTS idx_sim_state_history_sim ON sim_state_history (sim_id, created_at DESC);

-- ============================================================
-- TBL-15: policy_assignments
-- ============================================================
CREATE TABLE IF NOT EXISTS policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_assignments_sim ON policy_assignments (sim_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_version ON policy_assignments (policy_version_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_rollout ON policy_assignments (rollout_id);
CREATE INDEX IF NOT EXISTS idx_policy_assignments_coa ON policy_assignments (coa_status) WHERE coa_status != 'acked';

-- ============================================================
-- TBL-16: policy_rollouts
-- ============================================================
CREATE TABLE IF NOT EXISTS policy_rollouts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    previous_version_id UUID REFERENCES policy_versions(id),
    strategy VARCHAR(20) NOT NULL DEFAULT 'canary',
    stages JSONB NOT NULL,
    current_stage INTEGER NOT NULL DEFAULT 0,
    total_sims INTEGER NOT NULL,
    migrated_sims INTEGER NOT NULL DEFAULT 0,
    state VARCHAR(20) NOT NULL DEFAULT 'pending',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    rolled_back_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_policy_rollouts_version ON policy_rollouts (policy_version_id);
CREATE INDEX IF NOT EXISTS idx_policy_rollouts_state ON policy_rollouts (state);

-- Add FK from policy_assignments.rollout_id to policy_rollouts.id
ALTER TABLE policy_assignments ADD CONSTRAINT fk_policy_assignments_rollout
    FOREIGN KEY (rollout_id) REFERENCES policy_rollouts(id);

-- ============================================================
-- TBL-17: sessions (plain table; hypertable conversion in next migration)
-- ============================================================
CREATE TABLE IF NOT EXISTS sessions (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    nas_ip INET,
    framed_ip INET,
    calling_station_id VARCHAR(50),
    called_station_id VARCHAR(100),
    rat_type VARCHAR(10),
    session_state VARCHAR(20) NOT NULL DEFAULT 'active',
    auth_method VARCHAR(20),
    policy_version_id UUID,
    acct_session_id VARCHAR(100),
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ended_at TIMESTAMPTZ,
    terminate_cause VARCHAR(50),
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    packets_in BIGINT NOT NULL DEFAULT 0,
    packets_out BIGINT NOT NULL DEFAULT 0,
    last_interim_at TIMESTAMPTZ
);

-- ============================================================
-- TBL-18: cdrs (plain table; hypertable conversion in next migration)
-- ============================================================
CREATE TABLE IF NOT EXISTS cdrs (
    id BIGSERIAL,
    session_id UUID NOT NULL,
    sim_id UUID NOT NULL,
    tenant_id UUID NOT NULL,
    operator_id UUID NOT NULL,
    apn_id UUID,
    rat_type VARCHAR(10),
    record_type VARCHAR(20) NOT NULL,
    bytes_in BIGINT NOT NULL DEFAULT 0,
    bytes_out BIGINT NOT NULL DEFAULT 0,
    duration_sec INTEGER NOT NULL DEFAULT 0,
    usage_cost DECIMAL(12,4),
    carrier_cost DECIMAL(12,4),
    rate_per_mb DECIMAL(8,4),
    rat_multiplier DECIMAL(4,2) DEFAULT 1.0,
    timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- TBL-19: audit_logs (partitioned by created_at, monthly)
-- ============================================================
CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGSERIAL,
    tenant_id UUID NOT NULL,
    user_id UUID,
    api_key_id UUID,
    action VARCHAR(50) NOT NULL,
    entity_type VARCHAR(50) NOT NULL,
    entity_id VARCHAR(100) NOT NULL,
    before_data JSONB,
    after_data JSONB,
    diff JSONB,
    ip_address INET,
    user_agent TEXT,
    correlation_id UUID,
    hash VARCHAR(64) NOT NULL,
    prev_hash VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Create monthly partitions for current and upcoming months
CREATE TABLE IF NOT EXISTS audit_logs_2026_03 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_04 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_05 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE IF NOT EXISTS audit_logs_2026_06 PARTITION OF audit_logs
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');

CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_logs (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_entity ON audit_logs (tenant_id, entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_user ON audit_logs (tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_action ON audit_logs (tenant_id, action);
CREATE INDEX IF NOT EXISTS idx_audit_correlation ON audit_logs (correlation_id);

-- ============================================================
-- TBL-20: jobs
-- ============================================================
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    type VARCHAR(50) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'queued',
    priority INTEGER NOT NULL DEFAULT 5,
    payload JSONB NOT NULL,
    total_items INTEGER NOT NULL DEFAULT 0,
    processed_items INTEGER NOT NULL DEFAULT 0,
    failed_items INTEGER NOT NULL DEFAULT 0,
    progress_pct DECIMAL(5,2) NOT NULL DEFAULT 0,
    error_report JSONB,
    result JSONB,
    max_retries INTEGER NOT NULL DEFAULT 3,
    retry_count INTEGER NOT NULL DEFAULT 0,
    retry_backoff_sec INTEGER NOT NULL DEFAULT 30,
    scheduled_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    locked_by VARCHAR(100),
    locked_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_jobs_tenant_state ON jobs (tenant_id, state);
CREATE INDEX IF NOT EXISTS idx_jobs_state_priority ON jobs (state, priority) WHERE state = 'queued';
CREATE INDEX IF NOT EXISTS idx_jobs_scheduled ON jobs (scheduled_at) WHERE state = 'queued' AND scheduled_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_jobs_locked ON jobs (locked_by) WHERE locked_by IS NOT NULL;

-- ============================================================
-- TBL-21: notifications
-- ============================================================
CREATE TABLE IF NOT EXISTS notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID REFERENCES users(id),
    event_type VARCHAR(50) NOT NULL,
    scope_type VARCHAR(20) NOT NULL,
    scope_ref_id UUID,
    title VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    severity VARCHAR(10) NOT NULL DEFAULT 'info',
    channels_sent VARCHAR[] NOT NULL DEFAULT '{}',
    state VARCHAR(20) NOT NULL DEFAULT 'unread',
    read_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_tenant_user_state ON notifications (tenant_id, user_id, state) WHERE state = 'unread';
CREATE INDEX IF NOT EXISTS idx_notifications_tenant_time ON notifications (tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_scope ON notifications (scope_type, scope_ref_id);

-- ============================================================
-- TBL-22: notification_configs
-- ============================================================
CREATE TABLE IF NOT EXISTS notification_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    user_id UUID REFERENCES users(id),
    event_type VARCHAR(50) NOT NULL,
    scope_type VARCHAR(20) NOT NULL DEFAULT 'system',
    scope_ref_id UUID,
    channels JSONB NOT NULL,
    threshold_type VARCHAR(20),
    threshold_value DECIMAL(10,2),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notif_configs_tenant_event ON notification_configs (tenant_id, event_type);
CREATE INDEX IF NOT EXISTS idx_notif_configs_tenant_user ON notification_configs (tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_notif_configs_scope ON notification_configs (scope_type, scope_ref_id);

-- ============================================================
-- TBL-23: operator_health_logs (plain table; hypertable in next migration)
-- ============================================================
CREATE TABLE IF NOT EXISTS operator_health_logs (
    id BIGSERIAL,
    operator_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(20) NOT NULL,
    latency_ms INTEGER,
    error_message TEXT,
    circuit_state VARCHAR(20) NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_op_health_operator_time ON operator_health_logs (operator_id, checked_at DESC);

-- ============================================================
-- TBL-24: msisdn_pool
-- ============================================================
CREATE TABLE IF NOT EXISTS msisdn_pool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    msisdn VARCHAR(20) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    sim_id UUID,
    reserved_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_msisdn_pool_msisdn ON msisdn_pool (msisdn);
CREATE INDEX IF NOT EXISTS idx_msisdn_pool_tenant_op_state ON msisdn_pool (tenant_id, operator_id, state);
CREATE INDEX IF NOT EXISTS idx_msisdn_pool_sim ON msisdn_pool (sim_id) WHERE sim_id IS NOT NULL;

-- ============================================================
-- TBL-25: sim_segments
-- ============================================================
CREATE TABLE IF NOT EXISTS sim_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    filter_definition JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sim_segments_tenant_name ON sim_segments (tenant_id, name);

-- ============================================================
-- Updated_at trigger function
-- ============================================================
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Apply updated_at triggers to tables with updated_at column
CREATE TRIGGER trg_tenants_updated_at BEFORE UPDATE ON tenants
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_operators_updated_at BEFORE UPDATE ON operators
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_apns_updated_at BEFORE UPDATE ON apns
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_policies_updated_at BEFORE UPDATE ON policies
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_esim_profiles_updated_at BEFORE UPDATE ON esim_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER trg_notification_configs_updated_at BEFORE UPDATE ON notification_configs
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
