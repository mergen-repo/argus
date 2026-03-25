# Seed Data Report

**Generated:** 2026-03-23
**Seed File:** `migrations/seed/003_comprehensive_seed.sql`

## Schema Summary

The database has 25+ tables across 7 migration files. Key tables:
- Core: `tenants`, `users`, `operators`, `operator_grants`
- SIM: `sims` (partitioned by operator_id), `sim_state_history`, `esim_profiles`
- Network: `apns`, `ip_pools`, `ip_addresses`, `msisdn_pool`
- Policy: `policies`, `policy_versions`, `policy_assignments`, `policy_rollouts`
- Sessions: `sessions` (TimescaleDB hypertable), `cdrs` (TimescaleDB hypertable)
- Operations: `jobs`, `ota_commands`, `notifications`, `notification_configs`, `audit_logs` (partitioned by month)
- Analytics: `anomalies`, `operator_health_logs` (TimescaleDB hypertable)
- Segments: `sim_segments`
- Auth: `api_keys`, `user_sessions`

**Note:** `operator_grants` SoR columns (`sor_priority`, `cost_per_mb`, `region`) and `tenant_retention_config` table are defined in migrations but not present in the live schema. Seed handles this gracefully.

## Data Volume Table

| Table                | Count | Notes                                        |
|---------------------|-------|----------------------------------------------|
| tenants             | 3     | Argus Demo + Nar Teknoloji + Bosphorus IoT   |
| users               | 13    | 6 Nar + 5 Bosphorus + 2 system               |
| operators           | 4     | Turkcell, Vodafone TR, Turk Telekom, Mock     |
| operator_grants     | 6     | 3 for Nar + 2 for Bosphorus + 1 system       |
| apns                | 11    | 6 Nar + 5 Bosphorus                          |
| ip_pools            | 9     | 5 Nar + 4 Bosphorus (2 near capacity)        |
| sims                | 107   | 80 Nar + 27 Bosphorus, 5 states              |
| esim_profiles       | 10    | 6 Nar + 4 Bosphorus                          |
| sessions            | 255   | 55 active + 200 historical                   |
| cdrs                | 405   | Spanning ~30 days for chart data              |
| policies            | 8     | 5 Nar + 3 Bosphorus                          |
| policy_versions     | 10    | draft, active, rolling_out, rolled_back       |
| policy_rollouts     | 2     | 1 in_progress canary, 1 completed            |
| policy_assignments  | 69    | Linked SIMs to policy versions                |
| jobs                | 20    | queued, running, completed, failed            |
| notifications       | 47    | ~16 unread + ~31 read                        |
| audit_logs          | 250   | Hash-chained, spanning ~20 days               |
| api_keys            | 7     | 4 Nar + 3 Bosphorus                          |
| ota_commands        | 11    | Various statuses                              |
| sim_segments        | 7     | 4 Nar + 3 Bosphorus                          |
| msisdn_pool         | 60    | available + reserved + assigned               |
| anomalies           | 7     | data_spike, sim_cloning, auth_flood, nas_flood|
| operator_health_logs| 3850  | 20 entries x 3 operators (rolling)            |
| sim_state_history   | 72    | ordered, active, suspended, terminated        |
| notification_configs| 6     | Per-user event subscriptions                  |

## SIM Distribution

| State       | Count |
|-------------|-------|
| active      | 83    |
| suspended   | 8     |
| ordered     | 8     |
| terminated  | 6     |
| stolen_lost | 2     |

**By Operator:** Turkcell: 75, Vodafone TR: 24, Turk Telekom: 8
**By Tenant:** Nar Teknoloji: 80, Bosphorus IoT: 27

## Screen Verification Results

| Screen                | Route            | Status  | Notes                                          |
|-----------------------|-----------------|---------|------------------------------------------------|
| Login                 | /login          | OK      | Login works for both admin and tenant users    |
| Dashboard             | /               | OK      | Shows 80 SIMs, 35 sessions, SIM distribution chart, alert feed |
| SIM List              | /sims           | OK      | 80 SIMs with pagination, state filters, saved segments |
| APN List              | /apns           | OK      | 6 APNs visible for Nar tenant                  |
| Operators             | /operators      | OK      | 3 operators (Turkcell, Vodafone, TT)           |
| Sessions              | /sessions       | OK      | 55 active sessions                             |
| Policies              | /policies       | OK      | 5 policies with Turkish names/descriptions     |
| eSIM                  | /esim           | OK      | 10 eSIM profiles                               |
| Jobs                  | /jobs           | OK      | 12 jobs with varied types and states           |
| Audit Log             | /audit          | OK      | 250 entries with hash chain, "Load more" pagination |
| Notifications         | /notifications  | OK      | 47 notifications (unread + read mix)           |
| Users & Roles         | /settings/users | OK      | 6 users with different roles                   |
| API Keys              | /settings/api-keys | OK   | 4 API keys                                     |
| IP Pools              | /settings/ip-pools | OK   | 5 pools (some near capacity)                   |

## Data Characteristics

- **Turkish realism:** Turkish company names (Nar Teknoloji, Bosphorus IoT), Turkish person names, Turkish descriptions
- **IMSI format:** 286XXXXXXXXXXXX (286 = Turkey MCC, 01/02/03 = operator MNCs)
- **ICCID format:** 899XXXXXXXXXXXXXXXXXXXX (20 digits)
- **MSISDN format:** 905XXXXXXXXX (Turkish mobile prefix)
- **IP ranges:** 10.x.x.x private ranges per APN
- **Password:** All seed users use bcrypt-hashed "password123"
- **Audit hash chain:** SHA-256 based, properly chained from genesis hash
- **CDR time spread:** 30 days of data for analytics charts
- **Policy versions:** Multiple states (draft, active, rolling_out, rolled_back)
- **IP Pool utilization:** Camera pool at 91% (critical), Meter pool at 85% (warning)

## Known Issues

1. **Operator Health section on Dashboard shows "No operators configured"** — the dashboard API returns empty `operator_health` array; likely needs operator_grants query adjustment or a different API endpoint.
2. **Top APNs chart shows UUIDs instead of names** — dashboard API returns APN ID as `name` field instead of display name.
3. **Notifications 404 error** — `/api/v1/notifications/unread-count` returns 404; endpoint may not be implemented.
4. **WebSocket connection fails** — WSS connection to port 8081 is refused; WebSocket server may need configuration.
5. **Monthly Cost shows $0** — CDR cost aggregation may need the continuous aggregate views to be refreshed.

## Idempotency

The seed script is idempotent:
- Uses `ON CONFLICT DO NOTHING` for all INSERTs
- Includes cleanup section at top that removes previously seeded data
- Safe to re-run multiple times
