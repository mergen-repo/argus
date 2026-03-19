# Database: Audit, Jobs, Notifications Domain

## TBL-19: audit_logs

Append-only, partitioned by created_at (monthly). Hash chain for tamper detection.

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | BIGSERIAL | PK | Log entry ID |
| tenant_id | UUID | NOT NULL | Tenant |
| user_id | UUID | | Acting user (null for system actions) |
| api_key_id | UUID | | Acting API key (null for portal actions) |
| action | VARCHAR(50) | NOT NULL | create, update, delete, state_change, login, logout, login_failed, policy_rollout, bulk_op, coa_sent |
| entity_type | VARCHAR(50) | NOT NULL | sim, apn, policy, operator, user, tenant, ip_pool, api_key, session |
| entity_id | VARCHAR(100) | NOT NULL | Entity identifier |
| before_data | JSONB | | State before change (null for create) |
| after_data | JSONB | | State after change (null for delete) |
| diff | JSONB | | Computed diff (for updates) |
| ip_address | INET | | Client IP |
| user_agent | TEXT | | Client user agent |
| correlation_id | UUID | | Request correlation ID for distributed tracing |
| hash | VARCHAR(64) | NOT NULL | SHA-256 hash of this entry |
| prev_hash | VARCHAR(64) | NOT NULL | Hash of previous entry (chain link) |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Log timestamp |

Indexes:
- `idx_audit_tenant_time` on (tenant_id, created_at DESC)
- `idx_audit_tenant_entity` on (tenant_id, entity_type, entity_id)
- `idx_audit_tenant_user` on (tenant_id, user_id)
- `idx_audit_tenant_action` on (tenant_id, action)
- `idx_audit_correlation` on (correlation_id)

Partitioning:
```sql
CREATE TABLE audit_logs (
    ...
) PARTITION BY RANGE (created_at);
-- Monthly partitions: audit_logs_2026_03, audit_logs_2026_04, ...
-- Old partitions archived to S3 then detached
```

Hash chain computation:
```go
func computeHash(entry AuditEntry, prevHash string) string {
    data := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
        entry.TenantID, entry.UserID, entry.Action,
        entry.EntityType, entry.EntityID,
        entry.CreatedAt.Format(time.RFC3339Nano), prevHash)
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:])
}
```

Pseudonymization on purge:
```sql
UPDATE audit_logs SET
    before_data = anonymize_jsonb(before_data, ARRAY['imsi', 'msisdn', 'iccid']),
    after_data = anonymize_jsonb(after_data, ARRAY['imsi', 'msisdn', 'iccid']),
    diff = anonymize_jsonb(diff, ARRAY['imsi', 'msisdn', 'iccid'])
WHERE entity_type = 'sim' AND entity_id IN (SELECT id::text FROM sims WHERE state = 'purged');
```

---

## TBL-20: jobs

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Job identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| type | VARCHAR(50) | NOT NULL | bulk_import, bulk_state_change, bulk_policy_assign, bulk_esim_switch, policy_rollout, purge_sweep, ip_reclaim, sla_report, ota_command |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'queued' | queued, running, completed, failed, cancelled |
| priority | INTEGER | NOT NULL, DEFAULT 5 | 1 (highest) to 10 (lowest) |
| payload | JSONB | NOT NULL | Job-specific input data |
| total_items | INTEGER | NOT NULL, DEFAULT 0 | Total items to process |
| processed_items | INTEGER | NOT NULL, DEFAULT 0 | Items processed so far |
| failed_items | INTEGER | NOT NULL, DEFAULT 0 | Items that failed |
| progress_pct | DECIMAL(5,2) | NOT NULL, DEFAULT 0 | Completion percentage |
| error_report | JSONB | | Failed items detail [{row, iccid, error}] |
| result | JSONB | | Job result summary |
| max_retries | INTEGER | NOT NULL, DEFAULT 3 | Max retry count |
| retry_count | INTEGER | NOT NULL, DEFAULT 0 | Current retry count |
| retry_backoff_sec | INTEGER | NOT NULL, DEFAULT 30 | Backoff between retries |
| scheduled_at | TIMESTAMPTZ | | For scheduled/cron jobs |
| started_at | TIMESTAMPTZ | | Execution start time |
| completed_at | TIMESTAMPTZ | | Execution end time |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| created_by | UUID | FK → users.id | Creator |
| locked_by | VARCHAR(100) | | Worker instance ID (distributed lock) |
| locked_at | TIMESTAMPTZ | | Lock acquisition time |

Indexes:
- `idx_jobs_tenant_state` on (tenant_id, state)
- `idx_jobs_state_priority` on (state, priority) WHERE state = 'queued'
- `idx_jobs_scheduled` on (scheduled_at) WHERE state = 'queued' AND scheduled_at IS NOT NULL
- `idx_jobs_locked` on (locked_by) WHERE locked_by IS NOT NULL

---

## TBL-21: notifications

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Notification identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| user_id | UUID | FK → users.id | Target user (null for tenant-wide) |
| event_type | VARCHAR(50) | NOT NULL | quota_warning, quota_exceeded, operator_down, anomaly_detected, policy_rollout_complete, bulk_complete, sla_violation, compliance_alert, sim_state_change |
| scope_type | VARCHAR(20) | NOT NULL | sim, apn, operator, system |
| scope_ref_id | UUID | | Reference to SIM/APN/operator |
| title | VARCHAR(255) | NOT NULL | Notification title |
| body | TEXT | NOT NULL | Notification body (supports markdown) |
| severity | VARCHAR(10) | NOT NULL, DEFAULT 'info' | info, warning, critical |
| channels_sent | VARCHAR[] | NOT NULL, DEFAULT '{}' | Channels delivered to: in_app, email, telegram, webhook, sms |
| state | VARCHAR(20) | NOT NULL, DEFAULT 'unread' | unread, read, dismissed |
| read_at | TIMESTAMPTZ | | Read timestamp |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |

Indexes:
- `idx_notifications_tenant_user_state` on (tenant_id, user_id, state) WHERE state = 'unread'
- `idx_notifications_tenant_time` on (tenant_id, created_at DESC)
- `idx_notifications_scope` on (scope_type, scope_ref_id)

---

## TBL-22: notification_configs

| Column | Type | Constraints | Description |
|--------|------|------------|-------------|
| id | UUID | PK, DEFAULT gen_random_uuid() | Config identifier |
| tenant_id | UUID | FK → tenants.id, NOT NULL | Tenant |
| user_id | UUID | FK → users.id | User-specific config (null = tenant default) |
| event_type | VARCHAR(50) | NOT NULL | Event type to configure |
| scope_type | VARCHAR(20) | NOT NULL, DEFAULT 'system' | sim, apn, operator, system |
| scope_ref_id | UUID | | Specific SIM/APN/operator (null = all) |
| channels | JSONB | NOT NULL | {"in_app": true, "email": true, "telegram": false, "webhook": true, "sms": false} |
| threshold_type | VARCHAR(20) | | percentage, count, duration |
| threshold_value | DECIMAL(10,2) | | Threshold value (e.g., 80 for 80%) |
| enabled | BOOLEAN | NOT NULL, DEFAULT true | Config active flag |
| created_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Creation time |
| updated_at | TIMESTAMPTZ | NOT NULL, DEFAULT NOW() | Last update |

Indexes:
- `idx_notif_configs_tenant_event` on (tenant_id, event_type)
- `idx_notif_configs_tenant_user` on (tenant_id, user_id)
- `idx_notif_configs_scope` on (scope_type, scope_ref_id)
