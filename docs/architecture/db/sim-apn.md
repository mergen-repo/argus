# Database: SIM & APN Domain

## TBL-07: apns

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | APN identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| operator_id | UUID | FK → operators.id, NOT NULL | Operator |
| name | VARCHAR(100) | NOT NULL | APN name (e.g., "iot.fleet") |
| display_name | VARCHAR(255) | | Human-readable name |
| apn_type | VARCHAR(20) | NOT NULL | private_managed, operator_managed, customer_managed |
| supported_rat_types | VARCHAR[] | NOT NULL, DEFAULT '{}' | Allowed RAT types |
| default_policy_id | UUID | FK → policies.id | Default policy for new SIMs |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, archived |
| settings | JSONB | NOT NULL, DEFAULT '{}' | APN-specific config |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |
| created_by | UUID | FK → users.id | Creator |
| updated_by | UUID | FK → users.id | Last updater |

Indexes:
- `idx_apns_tenant_name` UNIQUE on (tenant_id, operator_id, name)
- `idx_apns_tenant_state` on (tenant_id, state)
- `idx_apns_operator` on (operator_id)

---

## TBL-08: ip_pools

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Pool identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| apn_id | UUID | FK → apns.id, NOT NULL | Associated APN |
| name | VARCHAR(100) | NOT NULL | Pool name |
| cidr_v4 | CIDR | | IPv4 range |
| cidr_v6 | CIDR | | IPv6 range |
| total_addresses | INTEGER | NOT NULL, DEFAULT 0 | Total available IPs |
| used_addresses | INTEGER | NOT NULL, DEFAULT 0 | Currently allocated IPs |
| alert_threshold_warning | INTEGER | NOT NULL, DEFAULT 80 | Warning at N% utilization |
| alert_threshold_critical | INTEGER | NOT NULL, DEFAULT 90 | Critical at N% |
| reclaim_grace_period_days | INTEGER | NOT NULL, DEFAULT 7 | Days before terminated SIM IP is reclaimed |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, exhausted, disabled |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |

Indexes:
- `idx_ip_pools_tenant_apn` on (tenant_id, apn_id)
- `idx_ip_pools_apn` on (apn_id)

---

## TBL-09: ip_addresses

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | IP identifier |
| pool_id | UUID | FK → ip_pools.id, NOT NULL | Parent pool |
| address_v4 | INET | | IPv4 address |
| address_v6 | INET | | IPv6 address |
| allocation_type | VARCHAR(10) | NOT NULL, DEFAULT 'dynamic' | static, dynamic |
| sim_id | UUID | FK → sims.id | Assigned SIM (null = available) |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'available' | available, allocated, reserved, reclaiming |
| allocated_at | TIMESTAMPTZ | | Allocation timestamp |
| reclaim_at | TIMESTAMPTZ | | Scheduled reclaim time |
| last_seen_at | TIMESTAMPTZ | NULL | Last RADIUS/Diameter keep-alive (FIX-223; writer deferred to D-121 / AAA accounting enrichment) |

Indexes:
- `idx_ip_addresses_pool_state` on (pool_id, state)
- `idx_ip_addresses_sim` on (sim_id) WHERE sim_id IS NOT NULL
- `idx_ip_addresses_v4` UNIQUE on (pool_id, address_v4) WHERE address_v4 IS NOT NULL
- `idx_ip_addresses_v6` UNIQUE on (pool_id, address_v6) WHERE address_v6 IS NOT NULL
- `idx_ip_addresses_reclaim` on (reclaim_at) WHERE state = 'reclaiming'

---

## TBL-10: sims

Partitioned by operator_id (list partitioning).

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | SIM identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| operator_id | UUID | FK → operators.id, NOT NULL | Current operator |
| apn_id | UUID | FK → apns.id | Assigned APN |
| iccid | VARCHAR(22) | NOT NULL | SIM card serial |
| imsi | VARCHAR(15) | NOT NULL | Subscriber identity |
| msisdn | VARCHAR(20) | | Phone number |
| ip_address_id | UUID | FK → ip_addresses.id | Assigned IP |
| policy_version_id | UUID | FK → policy_versions.id | Assigned policy version |
| esim_profile_id | UUID | FK → esim_profiles.id | eSIM profile (if eSIM) |
| sim_type | VARCHAR(10) | NOT NULL, DEFAULT 'physical' | physical, esim |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'ordered' | ordered, active, suspended, stolen_lost, terminated, purged |
| rat_type | VARCHAR(10) | | Current RAT: nb_iot, lte_m, lte, nr_5g |
| max_concurrent_sessions | INTEGER | NOT NULL, DEFAULT 1 | Session limit |
| session_idle_timeout_sec | INTEGER | NOT NULL, DEFAULT 3600 | Idle timeout |
| session_hard_timeout_sec | INTEGER | NOT NULL, DEFAULT 86400 | Hard timeout |
| metadata | JSONB | NOT NULL, DEFAULT '{}' | Custom metadata |
| activated_at | TIMESTAMPTZ | | First activation time |
| suspended_at | TIMESTAMPTZ | | Last suspension time |
| terminated_at | TIMESTAMPTZ | | Termination time |
| purge_at | TIMESTAMPTZ | | Scheduled purge time |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Import/creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |

Indexes:
- `idx_sims_iccid` UNIQUE on (iccid)
- `idx_sims_imsi` UNIQUE on (imsi)
- `idx_sims_msisdn` on (msisdn) WHERE msisdn IS NOT NULL
- `idx_sims_tenant_state` on (tenant_id, state)
- `idx_sims_tenant_operator` on (tenant_id, operator_id)
- `idx_sims_tenant_apn` on (tenant_id, apn_id)
- `idx_sims_tenant_policy` on (tenant_id, policy_version_id)
- `idx_sims_purge` on (purge_at) WHERE state = 'terminated'

Foreign Keys (FIX-206, migrations/20260420000002):
- `fk_sims_operator` on (operator_id) → `operators(id)` ON DELETE RESTRICT — blocks operator delete while SIMs reference it; enforces "reassign before delete".
- `fk_sims_apn` on (apn_id) → `apns(id)` ON DELETE SET NULL — APN deletion releases SIMs to default APN at next session.
- `fk_sims_ip_address` on (ip_address_id) → `ip_addresses(id)` ON DELETE SET NULL — IP release does not block SIM record.

Partitioning:
```sql
CREATE TABLE sims (
    ...
) PARTITION BY LIST (operator_id);

-- One partition per operator
CREATE TABLE sims_turkcell PARTITION OF sims FOR VALUES IN ('operator-uuid-turkcell');
CREATE TABLE sims_vodafone PARTITION OF sims FOR VALUES IN ('operator-uuid-vodafone');
CREATE TABLE sims_tt PARTITION OF sims FOR VALUES IN ('operator-uuid-tt');
CREATE TABLE sims_mock PARTITION OF sims FOR VALUES IN ('operator-uuid-mock');
```

---

## TBL-11: sim_state_history

Partitioned by created_at (monthly).

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | BIGSERIAL | PK | History entry ID |
| sim_id | UUID | NOT NULL | SIM reference |
| from_state | VARCHAR(20) | | Previous state (null for initial) |
| to_state | VARCHAR(20) | NOT NULL | New state |
| reason | VARCHAR(255) | | Transition reason |
| triggered_by | VARCHAR(20) | NOT NULL | user, policy, system, bulk_job |
| user_id | UUID | | User who triggered (if user) |
| job_id | UUID | | Job that triggered (if bulk) |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Transition time |

Indexes:
- `idx_sim_state_history_sim` on (sim_id, created_at DESC)

Partitioning:
```sql
CREATE TABLE sim_state_history (
    ...
) PARTITION BY RANGE (created_at);
-- Monthly partitions auto-created
```

---

## TBL-12: esim_profiles

Multi-profile eSIM table. A single SIM may have up to 8 profiles (GSMA SGP.22 cap). Exactly one profile per SIM may be in `enabled` state, enforced by partial unique index.

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Profile identifier |
| sim_id | UUID | FK → sims.id, NOT NULL | Parent SIM (multi-profile: no plain UNIQUE) |
| profile_id | VARCHAR(64) | | Operator/SM-DP+ profile identifier (for dedup) |
| eid | VARCHAR(32) | NOT NULL | eUICC identifier |
| sm_dp_plus_id | VARCHAR(255) | | SM-DP+ profile reference |
| operator_id | UUID | FK → operators.id, NOT NULL | Profile operator |
| profile_state | VARCHAR(20) | NOT NULL, DEFAULT 'available', CHECK (profile_state IN ('available','enabled','disabled','deleted')) | available (loaded, inactive), enabled (active), disabled (operator-deactivated), deleted (soft-deleted) |
| iccid_on_profile | VARCHAR(22) | | Profile-specific ICCID |
| last_provisioned_at | TIMESTAMPTZ | | Last SM-DP+ operation time |
| last_error | TEXT | | Last provisioning error |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |

Indexes:
- `idx_esim_profiles_sim_enabled` PARTIAL UNIQUE on (sim_id) WHERE profile_state = 'enabled' — enforces one active profile per SIM
- `idx_esim_profiles_sim_profile` PARTIAL UNIQUE on (sim_id, profile_id) WHERE profile_id IS NOT NULL — prevents duplicate profile_id per SIM
- `idx_esim_profiles_sim_state` on (sim_id, profile_state) — covers CountBySIM and filtered list queries
- `idx_esim_profiles_eid` on (eid)
- `idx_esim_profiles_operator` on (operator_id)

Migration: `20260412000002_esim_multiprofile.up.sql` / `.down.sql`

---

## TBL-24: msisdn_pool

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Entry identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| operator_id | UUID | FK → operators.id, NOT NULL | Operator |
| msisdn | VARCHAR(20) | NOT NULL | Phone number |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'available' | available, assigned, reserved |
| sim_id | UUID | FK → sims.id | Assigned SIM |
| reserved_until | TIMESTAMPTZ | | Reservation expiry |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |

Indexes:
- `idx_msisdn_pool_msisdn` UNIQUE on (msisdn)
- `idx_msisdn_pool_tenant_op_state` on (tenant_id, operator_id, state)
- `idx_msisdn_pool_sim` on (sim_id) WHERE sim_id IS NOT NULL

---

## TBL-25: sim_segments

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Segment identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| name | VARCHAR(100) | NOT NULL | Segment name |
| filter_definition | JSONB | NOT NULL, DEFAULT '{}' | Filter criteria (operator_id, state, apn_id, rat_type) |
| created_by | UUID | FK → users.id | Creator |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |

Indexes:
- `idx_sim_segments_tenant_name` UNIQUE on (tenant_id, name)
