# Database: Operator Domain

## TBL-05: operators

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Operator identifier |
| name | VARCHAR(100) | NOT NULL, UNIQUE | Operator name (e.g., "Turkcell") |
| code | VARCHAR(20) | NOT NULL, UNIQUE | Short code (e.g., "turkcell") |
| mcc | VARCHAR(3) | NOT NULL | Mobile Country Code |
| mnc | VARCHAR(3) | NOT NULL | Mobile Network Code |
| adapter_type | VARCHAR(30) | NOT NULL | mock, radius, diameter, sba |
| adapter_config | JSONB | NOT NULL, DEFAULT '{}' | Connection config (host, port, shared_secret — encrypted) |
| sm_dp_plus_url | VARCHAR(500) | | SM-DP+ API endpoint |
| sm_dp_plus_config | JSONB | DEFAULT '{}' | SM-DP+ auth config (encrypted) |
| supported_rat_types | VARCHAR[] | NOT NULL, DEFAULT '{}' | Supported RAT types: nb_iot, lte_m, lte, nr_5g |
| health_status | VARCHAR(20) | NOT NULL, DEFAULT 'unknown' | healthy, degraded, down, unknown |
| health_check_interval_sec | INTEGER | NOT NULL, DEFAULT 30 | Heartbeat interval |
| failover_policy | VARCHAR(20) | NOT NULL, DEFAULT 'reject' | reject, fallback_to_next, queue_with_timeout |
| failover_timeout_ms | INTEGER | NOT NULL, DEFAULT 5000 | Queue timeout for queue_with_timeout policy |
| circuit_breaker_threshold | INTEGER | NOT NULL, DEFAULT 5 | Consecutive failures before circuit opens |
| circuit_breaker_recovery_sec | INTEGER | NOT NULL, DEFAULT 60 | Recovery window |
| sla_uptime_target | DECIMAL(5,2) | DEFAULT 99.90 | SLA uptime percentage target |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, disabled |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |

Indexes:
- `idx_operators_code` UNIQUE on (code)
- `idx_operators_mcc_mnc` UNIQUE on (mcc, mnc)
- `idx_operators_state` on (state)

---

## TBL-06: operator_grants

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Grant identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| operator_id | UUID | FK → operators.id, NOT NULL | Operator |
| enabled | BOOLEAN | NOT NULL, DEFAULT true | Grant active flag |
| granted_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Grant time |
| granted_by | UUID | FK → users.id | Granting user |
| sor_priority | INTEGER | NOT NULL, DEFAULT 100 | SoR routing priority (lower = preferred) |
| cost_per_mb | DECIMAL(10,6) | | Cost per MB for this operator-tenant grant |
| supported_rat_types | TEXT[] | NOT NULL, DEFAULT '{}' | RAT types supported under this grant |

Indexes:
- `idx_operator_grants_tenant_op` UNIQUE on (tenant_id, operator_id)
- `idx_operator_grants_tenant` on (tenant_id) WHERE enabled = true
- `idx_operator_grants_sor` on (tenant_id, sor_priority) WHERE enabled = true

---

## TBL-23: operator_health_logs

TimescaleDB hypertable — partitioned by checked_at.

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | BIGSERIAL | PK | Log entry ID |
| operator_id | UUID | NOT NULL | Operator |
| checked_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Check timestamp |
| status | VARCHAR(20) | NOT NULL | healthy, degraded, down |
| latency_ms | INTEGER | | Response latency |
| error_message | TEXT | | Error detail if unhealthy |
| circuit_state | VARCHAR(20) | NOT NULL | closed, open, half_open |

Indexes:
- `idx_op_health_operator_time` on (operator_id, checked_at DESC)

TimescaleDB:
- `SELECT create_hypertable('operator_health_logs', 'checked_at');`
- Compression after 7 days
- Retention: 90 days (configurable)
