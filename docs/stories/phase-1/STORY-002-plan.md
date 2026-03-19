# Implementation Plan: STORY-002 - Core Database Schema & Migrations

## Goal
Create all 24+ database tables defined in the architecture docs via reversible migrations, with proper indexes, constraints, foreign keys, partitioning, TimescaleDB hypertables, continuous aggregates, and idempotent seed data.

## Architecture Context

### Components Involved
- **Migrations** (`migrations/`): golang-migrate SQL files (up/down pairs)
- **Seed data** (`migrations/seed/`): Idempotent SQL seed files
- **Makefile targets**: `make db-migrate`, `make db-seed` (already defined, exec via Docker)

### Existing State
- Migration `20260320000001_init_extensions` already creates TimescaleDB + uuid-ossp extensions
- Migration `20260319000001_sim_segments` creates index on sim_segments table (but the table itself is not created yet)
- Existing Go code in `internal/store/` references tables: `sims`, `sim_segments`, `jobs`, `msisdn_pool` -- these must be created by migrations
- The `sim_segments` migration only creates an index, not the table. The table needs to be created as part of core schema.

### Database Schema

All tables from architecture docs (TBL-01 through TBL-25):

#### TBL-01: tenants
```sql
CREATE TABLE tenants (
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
CREATE INDEX idx_tenants_domain ON tenants (domain);
CREATE INDEX idx_tenants_state ON tenants (state);
```

#### TBL-02: users
```sql
CREATE TABLE users (
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
CREATE UNIQUE INDEX idx_users_tenant_email ON users (tenant_id, email);
CREATE INDEX idx_users_tenant_role ON users (tenant_id, role);
CREATE INDEX idx_users_state ON users (state);
```

#### TBL-03: user_sessions
```sql
CREATE TABLE user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id),
    refresh_token_hash VARCHAR(255) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_user_sessions_user ON user_sessions (user_id);
CREATE INDEX idx_user_sessions_expires ON user_sessions (expires_at) WHERE revoked_at IS NULL;
```

#### TBL-04: api_keys
```sql
CREATE TABLE api_keys (
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
CREATE INDEX idx_api_keys_tenant ON api_keys (tenant_id);
CREATE INDEX idx_api_keys_prefix ON api_keys (key_prefix);
CREATE INDEX idx_api_keys_active ON api_keys (tenant_id) WHERE revoked_at IS NULL AND (expires_at IS NULL OR expires_at > NOW());
```

#### TBL-05: operators
```sql
CREATE TABLE operators (
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
CREATE UNIQUE INDEX idx_operators_code ON operators (code);
CREATE UNIQUE INDEX idx_operators_mcc_mnc ON operators (mcc, mnc);
CREATE INDEX idx_operators_state ON operators (state);
```

#### TBL-06: operator_grants
```sql
CREATE TABLE operator_grants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by UUID REFERENCES users(id)
);
CREATE UNIQUE INDEX idx_operator_grants_tenant_op ON operator_grants (tenant_id, operator_id);
CREATE INDEX idx_operator_grants_tenant ON operator_grants (tenant_id) WHERE enabled = true;
```

#### TBL-07: apns
```sql
CREATE TABLE apns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    name VARCHAR(100) NOT NULL,
    display_name VARCHAR(255),
    apn_type VARCHAR(20) NOT NULL,
    supported_rat_types VARCHAR[] NOT NULL DEFAULT '{}',
    default_policy_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by UUID REFERENCES users(id),
    updated_by UUID REFERENCES users(id)
);
CREATE UNIQUE INDEX idx_apns_tenant_name ON apns (tenant_id, operator_id, name);
CREATE INDEX idx_apns_tenant_state ON apns (tenant_id, state);
CREATE INDEX idx_apns_operator ON apns (operator_id);
```

#### TBL-08: ip_pools
```sql
CREATE TABLE ip_pools (
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
CREATE INDEX idx_ip_pools_tenant_apn ON ip_pools (tenant_id, apn_id);
CREATE INDEX idx_ip_pools_apn ON ip_pools (apn_id);
```

#### TBL-09: ip_addresses
```sql
CREATE TABLE ip_addresses (
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
CREATE INDEX idx_ip_addresses_pool_state ON ip_addresses (pool_id, state);
CREATE INDEX idx_ip_addresses_sim ON ip_addresses (sim_id) WHERE sim_id IS NOT NULL;
CREATE UNIQUE INDEX idx_ip_addresses_v4 ON ip_addresses (pool_id, address_v4) WHERE address_v4 IS NOT NULL;
CREATE UNIQUE INDEX idx_ip_addresses_v6 ON ip_addresses (pool_id, address_v6) WHERE address_v6 IS NOT NULL;
CREATE INDEX idx_ip_addresses_reclaim ON ip_addresses (reclaim_at) WHERE state = 'reclaiming';
```

#### TBL-13: policies
```sql
CREATE TABLE policies (
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
CREATE UNIQUE INDEX idx_policies_tenant_name ON policies (tenant_id, name);
CREATE INDEX idx_policies_tenant_scope ON policies (tenant_id, scope);
CREATE INDEX idx_policies_state ON policies (state);
```

#### TBL-14: policy_versions
```sql
CREATE TABLE policy_versions (
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
CREATE UNIQUE INDEX idx_policy_versions_policy_ver ON policy_versions (policy_id, version);
CREATE INDEX idx_policy_versions_policy_state ON policy_versions (policy_id, state);
```

#### TBL-10: sims (partitioned by operator_id)
```sql
CREATE TABLE sims (
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
CREATE UNIQUE INDEX idx_sims_iccid ON sims (iccid);
CREATE UNIQUE INDEX idx_sims_imsi ON sims (imsi);
CREATE INDEX idx_sims_msisdn ON sims (msisdn) WHERE msisdn IS NOT NULL;
CREATE INDEX idx_sims_tenant_state ON sims (tenant_id, state);
CREATE INDEX idx_sims_tenant_operator ON sims (tenant_id, operator_id);
CREATE INDEX idx_sims_tenant_apn ON sims (tenant_id, apn_id);
CREATE INDEX idx_sims_tenant_policy ON sims (tenant_id, policy_version_id);
CREATE INDEX idx_sims_purge ON sims (purge_at) WHERE state = 'terminated';
```

#### TBL-11: sim_state_history (partitioned by created_at, monthly)
```sql
CREATE TABLE sim_state_history (
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
CREATE INDEX idx_sim_state_history_sim ON sim_state_history (sim_id, created_at DESC);
```

#### TBL-12: esim_profiles
```sql
CREATE TABLE esim_profiles (
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
CREATE UNIQUE INDEX idx_esim_profiles_sim ON esim_profiles (sim_id);
CREATE INDEX idx_esim_profiles_eid ON esim_profiles (eid);
CREATE INDEX idx_esim_profiles_operator ON esim_profiles (operator_id);
```

#### TBL-15: policy_assignments
```sql
CREATE TABLE policy_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sim_id UUID NOT NULL,
    policy_version_id UUID NOT NULL REFERENCES policy_versions(id),
    rollout_id UUID,
    assigned_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    coa_sent_at TIMESTAMPTZ,
    coa_status VARCHAR(20) DEFAULT 'pending'
);
CREATE UNIQUE INDEX idx_policy_assignments_sim ON policy_assignments (sim_id);
CREATE INDEX idx_policy_assignments_version ON policy_assignments (policy_version_id);
CREATE INDEX idx_policy_assignments_rollout ON policy_assignments (rollout_id);
CREATE INDEX idx_policy_assignments_coa ON policy_assignments (coa_status) WHERE coa_status != 'acked';
```

#### TBL-16: policy_rollouts
```sql
CREATE TABLE policy_rollouts (
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
CREATE INDEX idx_policy_rollouts_version ON policy_rollouts (policy_version_id);
CREATE INDEX idx_policy_rollouts_state ON policy_rollouts (state);
```

#### TBL-17: sessions (TimescaleDB hypertable)
```sql
CREATE TABLE sessions (
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
```

#### TBL-18: cdrs (TimescaleDB hypertable)
```sql
CREATE TABLE cdrs (
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
```

#### TBL-19: audit_logs (partitioned by created_at, monthly)
```sql
CREATE TABLE audit_logs (
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
CREATE INDEX idx_audit_tenant_time ON audit_logs (tenant_id, created_at DESC);
CREATE INDEX idx_audit_tenant_entity ON audit_logs (tenant_id, entity_type, entity_id);
CREATE INDEX idx_audit_tenant_user ON audit_logs (tenant_id, user_id);
CREATE INDEX idx_audit_tenant_action ON audit_logs (tenant_id, action);
CREATE INDEX idx_audit_correlation ON audit_logs (correlation_id);
```

#### TBL-20: jobs
```sql
CREATE TABLE jobs (
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
CREATE INDEX idx_jobs_tenant_state ON jobs (tenant_id, state);
CREATE INDEX idx_jobs_state_priority ON jobs (state, priority) WHERE state = 'queued';
CREATE INDEX idx_jobs_scheduled ON jobs (scheduled_at) WHERE state = 'queued' AND scheduled_at IS NOT NULL;
CREATE INDEX idx_jobs_locked ON jobs (locked_by) WHERE locked_by IS NOT NULL;
```

#### TBL-21: notifications
```sql
CREATE TABLE notifications (
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
CREATE INDEX idx_notifications_tenant_user_state ON notifications (tenant_id, user_id, state) WHERE state = 'unread';
CREATE INDEX idx_notifications_tenant_time ON notifications (tenant_id, created_at DESC);
CREATE INDEX idx_notifications_scope ON notifications (scope_type, scope_ref_id);
```

#### TBL-22: notification_configs
```sql
CREATE TABLE notification_configs (
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
CREATE INDEX idx_notif_configs_tenant_event ON notification_configs (tenant_id, event_type);
CREATE INDEX idx_notif_configs_tenant_user ON notification_configs (tenant_id, user_id);
CREATE INDEX idx_notif_configs_scope ON notification_configs (scope_type, scope_ref_id);
```

#### TBL-23: operator_health_logs (TimescaleDB hypertable)
```sql
CREATE TABLE operator_health_logs (
    id BIGSERIAL,
    operator_id UUID NOT NULL,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status VARCHAR(20) NOT NULL,
    latency_ms INTEGER,
    error_message TEXT,
    circuit_state VARCHAR(20) NOT NULL
);
CREATE INDEX idx_op_health_operator_time ON operator_health_logs (operator_id, checked_at DESC);
```

#### TBL-24: msisdn_pool
```sql
CREATE TABLE msisdn_pool (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID NOT NULL REFERENCES operators(id),
    msisdn VARCHAR(20) NOT NULL,
    state VARCHAR(20) NOT NULL DEFAULT 'available',
    sim_id UUID,
    reserved_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_msisdn_pool_msisdn ON msisdn_pool (msisdn);
CREATE INDEX idx_msisdn_pool_tenant_op_state ON msisdn_pool (tenant_id, operator_id, state);
CREATE INDEX idx_msisdn_pool_sim ON msisdn_pool (sim_id) WHERE sim_id IS NOT NULL;
```

#### TBL-25: sim_segments
```sql
CREATE TABLE sim_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    name VARCHAR(100) NOT NULL,
    filter_definition JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX idx_sim_segments_tenant_name ON sim_segments (tenant_id, name);
```

### Continuous Aggregates
```sql
-- cdrs_hourly
CREATE MATERIALIZED VIEW cdrs_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', timestamp) AS bucket,
    tenant_id, operator_id, apn_id, rat_type,
    COUNT(*) AS record_count,
    SUM(bytes_in) AS total_bytes_in,
    SUM(bytes_out) AS total_bytes_out,
    SUM(usage_cost) AS total_usage_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id, apn_id, rat_type;

-- cdrs_daily
CREATE MATERIALIZED VIEW cdrs_daily
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', timestamp) AS bucket,
    tenant_id, operator_id,
    COUNT(DISTINCT sim_id) AS active_sims,
    SUM(bytes_in + bytes_out) AS total_bytes,
    SUM(usage_cost) AS total_cost,
    SUM(carrier_cost) AS total_carrier_cost
FROM cdrs
GROUP BY bucket, tenant_id, operator_id;
```

### Seed Data

#### SEED-01: Admin User
- Tenant: "Argus Demo" (id generated)
- Email: admin@argus.io
- Password: admin (bcrypt cost 12)
- Role: super_admin
- Idempotent: ON CONFLICT DO NOTHING

#### SEED-02: System Data
- Operator: Mock Simulator (code: mock, mcc: 999, mnc: 99, adapter: mock)
- Operator grant linking demo tenant to mock operator
- All idempotent

## Prerequisites
- [x] STORY-001 completed (Docker, PG, Redis, NATS running)
- [x] TimescaleDB + uuid-ossp extensions created (migration 20260320000001)

## Tasks

### Task 1: Core Schema Migration (Platform + Operator tables)
- **Files:** Create `migrations/20260320000002_core_schema.up.sql`, Create `migrations/20260320000002_core_schema.down.sql`
- **Depends on:** --
- **Complexity:** high
- **Pattern ref:** Read `migrations/20260320000001_init_extensions.up.sql`
- **Context refs:** Database Schema (TBL-01 through TBL-25)
- **What:** Create the main migration file containing ALL 24+ tables (TBL-01 through TBL-25) with all columns, constraints, indexes, and partitioning. Tables must be created in dependency order. The `sims` table uses LIST partitioning by operator_id. The `sim_state_history` and `audit_logs` tables use RANGE partitioning by created_at. Create initial partitions for sim_state_history (current month + next 3 months) and audit_logs (current month + next 3 months). The `sessions` and `cdrs` and `operator_health_logs` tables are plain tables here (hypertables created in next migration). The down migration must drop all tables in reverse dependency order.
- **Note:** The existing `20260319000001_sim_segments` migration only creates an index -- the `sim_segments` table itself must be included in the core schema. We need to also handle: (a) replace the old sim_segments migration or (b) include sim_segments table in core schema (before the index migration runs).  Since golang-migrate runs in order and 20260319 < 20260320, the sim_segments index migration runs first but the table doesn't exist yet. We need to renumber the sim_segments migration to run AFTER core schema. Best approach: delete the old sim_segments migration files and include everything in core schema.
- **Verify:** `psql -h localhost -p 5450 -U argus -d argus -c "\dt" | wc -l` shows all tables

### Task 2: TimescaleDB Hypertables Migration
- **Files:** Create `migrations/20260320000003_timescaledb_hypertables.up.sql`, Create `migrations/20260320000003_timescaledb_hypertables.down.sql`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260320000001_init_extensions.up.sql`
- **Context refs:** Database Schema (TBL-17, TBL-18, TBL-23)
- **What:** Convert sessions, cdrs, and operator_health_logs into TimescaleDB hypertables. Add compression policies (sessions: 30 days, cdrs: 7 days, operator_health_logs: 7 days). Add retention policies (operator_health_logs: 90 days). Down migration must reverse (drop policies, but hypertables can't be reverted easily -- note this).
- **Verify:** `psql` query `SELECT hypertable_name FROM timescaledb_information.hypertables` shows 3 entries

### Task 3: Continuous Aggregates Migration
- **Files:** Create `migrations/20260320000004_continuous_aggregates.up.sql`, Create `migrations/20260320000004_continuous_aggregates.down.sql`
- **Depends on:** Task 2
- **Complexity:** medium
- **Pattern ref:** Read `migrations/20260320000001_init_extensions.up.sql`
- **Context refs:** Continuous Aggregates
- **What:** Create `cdrs_hourly` and `cdrs_daily` continuous aggregate materialized views. Add refresh policies. Down migration drops both views.
- **Verify:** `psql` query for materialized views shows cdrs_hourly and cdrs_daily

### Task 4: Seed Data Files
- **Files:** Create `migrations/seed/001_admin_user.sql`, Create `migrations/seed/002_system_data.sql`
- **Depends on:** Task 1
- **Complexity:** medium
- **Context refs:** Seed Data
- **What:** Create seed files. SEED-01: Insert demo tenant "Argus Demo", then insert admin user (admin@argus.io, bcrypt hash of "admin" at cost 12, role super_admin). SEED-02: Insert mock operator (code: mock, mcc: 999, mnc: 99, adapter_type: mock), operator grant for demo tenant. All use ON CONFLICT DO NOTHING for idempotency. The bcrypt hash for "admin" must be a valid pre-computed hash.
- **Verify:** Seeds execute without errors and are re-runnable

### Task 5: Remove Old sim_segments Migration + Verify Build
- **Files:** Delete `migrations/20260319000001_sim_segments.up.sql`, Delete `migrations/20260319000001_sim_segments.down.sql`
- **Depends on:** Task 1
- **Complexity:** low
- **Context refs:** Architecture Context > Existing State
- **What:** Remove the orphaned sim_segments migration files (the table + index are now part of core schema in Task 1). Verify that `go build ./...` still passes.
- **Verify:** `go build ./...` succeeds, no orphaned migration files

## Acceptance Criteria Mapping
| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| All 24 tables created | Task 1 | Gate check |
| All indexes created | Task 1 | Gate check |
| SIM table partitioned by operator_id | Task 1 | Gate check |
| sim_state_history partitioned by created_at | Task 1 | Gate check |
| audit_logs partitioned by created_at | Task 1 | Gate check |
| sessions as TimescaleDB hypertable | Task 2 | Gate check |
| cdrs as TimescaleDB hypertable | Task 2 | Gate check |
| operator_health_logs as TimescaleDB hypertable | Task 2 | Gate check |
| CDR continuous aggregates | Task 3 | Gate check |
| All foreign key constraints | Task 1 | Gate check |
| All migrations have up/down | Tasks 1-3 | Gate check |
| SEED-01: Admin user | Task 4 | Gate check |
| SEED-02: System data | Task 4 | Gate check |
| make db-migrate executes cleanly | All tasks | Gate check |
| Seeds are idempotent | Task 4 | Gate check |

## Story-Specific Compliance Rules
- DB: All migrations reversible (up + down)
- DB: Migration naming: `YYYYMMDDHHMMSS_description.up.sql` / `.down.sql`
- DB: All DB queries scoped by tenant_id (enforced in store layer)
- DB: snake_case naming, plural table names
- Business: Admin credentials admin@argus.io / admin per CLAUDE.md

## Risks & Mitigations
- **Risk:** TimescaleDB hypertable creation requires empty tables. Mitigation: Create hypertables in separate migration that runs after table creation.
- **Risk:** Partitioned tables (sims, sim_state_history, audit_logs) need at least one partition to accept data. Mitigation: Create default/initial partitions in migration.
- **Risk:** Circular FK between policies.current_version_id and policy_versions.policy_id. Mitigation: Add policies.current_version_id FK as ALTER TABLE after both tables exist, or omit FK constraint on current_version_id (it's a soft reference).
- **Risk:** sims table FK to ip_addresses, esim_profiles, policy_versions can't be defined inline due to partitioning. Mitigation: Omit FKs on partitioned sims table columns that reference other tables (PostgreSQL limitation with partitioned tables and FKs to non-partitioned tables).
