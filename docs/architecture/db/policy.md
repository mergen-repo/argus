# Database: Policy Domain

## TBL-13: policies

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Policy identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| name | VARCHAR(100) | NOT NULL | Policy name |
| description | TEXT | | Policy description |
| scope | VARCHAR(20) | NOT NULL | global, operator, apn, sim |
| scope_ref_id | UUID | | Reference to operator/apn/sim depending on scope |
| current_version_id | UUID | FK → policy_versions.id | Currently active version |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'active' | active, disabled, archived |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |
| created_by | UUID | FK → users.id | Creator |

Indexes:
- `idx_policies_tenant_name` UNIQUE on (tenant_id, name)
- `idx_policies_tenant_scope` on (tenant_id, scope)
- `idx_policies_state` on (state)

---

## TBL-14: policy_versions

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Version identifier |
| policy_id | UUID | FK → policies.id, NOT NULL | Parent policy |
| version | INTEGER | NOT NULL | Sequential version number |
| dsl_content | TEXT | NOT NULL | Policy DSL source code |
| compiled_rules | JSONB | NOT NULL | Compiled/parsed rule tree for fast evaluation |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'draft' | draft, active, rolling_out, rolled_back, superseded |
| affected_sim_count | INTEGER | | Calculated during dry-run |
| dry_run_result | JSONB | | Dry-run simulation output |
| activated_at | TIMESTAMPTZ | | When this version went active |
| rolled_back_at | TIMESTAMPTZ | | If rolled back |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| created_by | UUID | FK → users.id | Creator |

Indexes:
- `idx_policy_versions_policy_ver` UNIQUE on (policy_id, version)
- `idx_policy_versions_policy_state` on (policy_id, state)

---

## TBL-15: policy_assignments

Tracks which policy version each SIM is currently using (especially during staged rollout).

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Assignment identifier |
| sim_id | UUID | NOT NULL | SIM reference |
| policy_version_id | UUID | FK → policy_versions.id, NOT NULL | Assigned version |
| rollout_id | UUID | FK → policy_rollouts.id | Rollout that assigned this (null if direct) |
| assigned_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Assignment time |
| coa_sent_at | TIMESTAMPTZ | | CoA confirmation time |
| coa_status | VARCHAR(20) | DEFAULT 'pending' | pending, sent, acked, failed |

Indexes:
- `idx_policy_assignments_sim` UNIQUE on (sim_id)
- `idx_policy_assignments_version` on (policy_version_id)
- `idx_policy_assignments_rollout` on (rollout_id)
- `idx_policy_assignments_coa` on (coa_status) WHERE coa_status != 'acked'

---

## TBL-16: policy_rollouts

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Rollout identifier |
| policy_version_id | UUID | FK → policy_versions.id, NOT NULL | Version being rolled out |
| previous_version_id | UUID | FK → policy_versions.id | Version being replaced |
| strategy | VARCHAR(20) | NOT NULL, DEFAULT 'canary' | canary, immediate |
| stages | JSONB | NOT NULL | Stage definitions: [{"pct": 1}, {"pct": 10}, {"pct": 100}] |
| current_stage | INTEGER | NOT NULL, DEFAULT 0 | Current stage index |
| total_sims | INTEGER | NOT NULL | Total SIMs affected |
| migrated_sims | INTEGER | NOT NULL, DEFAULT 0 | SIMs migrated so far |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'pending' | pending, in_progress, paused, completed, rolled_back |
| started_at | TIMESTAMPTZ | | Rollout start time |
| completed_at | TIMESTAMPTZ | | Rollout completion time |
| rolled_back_at | TIMESTAMPTZ | | Rollback time |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| created_by | UUID | FK → users.id | Initiator |

Indexes:
- `idx_policy_rollouts_version` on (policy_version_id)
- `idx_policy_rollouts_state` on (state)

## Policy DSL Example

```
POLICY "iot-fleet-standard" {
  MATCH {
    apn IN ("iot.fleet", "iot.meter")
    rat_type IN (nb_iot, lte_m)
  }

  RULES {
    bandwidth_down = 1mbps
    bandwidth_up = 256kbps

    WHEN usage > 500MB {
      bandwidth_down = 256kbps
      bandwidth_up = 64kbps
      ACTION notify(quota_warning, 80%)
    }

    WHEN usage > 1GB {
      ACTION throttle(64kbps)
      ACTION notify(quota_exceeded, 100%)
    }

    WHEN time_of_day IN (00:00-06:00) {
      bandwidth_down = 2mbps  # off-peak bonus
    }
  }

  CHARGING {
    model = postpaid
    rate_per_mb = 0.01
    rat_type_multiplier {
      nb_iot = 0.5
      lte_m = 1.0
      lte = 2.0
      nr_5g = 3.0
    }
  }
}
```
