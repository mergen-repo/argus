# Product Definition — Argus

## Problem Statement

Enterprise IoT/M2M customers managing large-scale SIM fleets (10M+) across multiple mobile operators face a fragmented, manual, and expensive reality:

1. **Fragmented management** — Each operator has its own portal, API, and workflows. Managing 3-4 operators means 3-4 separate logins, 3-4 different UIs, no unified view.
2. **No policy automation** — QoS rules, FUP enforcement, and charging policies are configured manually per operator. A policy change across 10M SIMs is operationally impossible.
3. **No unified analytics** — Usage data, cost allocation, and anomaly detection are scattered across operator silos. No cross-operator visibility.
4. **No eSIM orchestration** — eSIM profile switching between operators requires manual coordination with each operator's SM-DP+ platform.
5. **Scale limitations** — Existing tools (FreeRADIUS, Mobile ITM SCM) lack the combination of protocol depth (Diameter, 5G), lifecycle management, and analytics needed at 10M+ scale.

**Impact:** Enterprises overspend on connectivity, can't enforce policies at scale, miss anomalies, and waste operational hours on manual tasks.

## Solution Overview

Argus is a 5-layer platform that unifies AAA protocol handling, SIM/APN lifecycle, multi-operator orchestration, policy enforcement, and business intelligence into a single product:

```
Portal & API ─── Unified management interface
     │
Layer 5: BI & Analytics ─── Real-time dashboards, anomaly detection, CDR/billing
     │
Layer 4: Policy Engine ─── QoS, FUP, charging rules, staged rollout
     │
Layer 3: Multi-Operator ─── SoR, failover, IMSI routing, operator adapters
     │
Layer 2: SIM & APN ─── Lifecycle, eSIM, IPAM, bulk ops, OTA
     │
Layer 1: AAA Core ─── RADIUS, Diameter, 5G SBA, EAP-SIM/AKA
```

## Key Features

### Must Have (v1 — all ship together)

#### AAA Core
- F-001: RADIUS server (RFC 2865/2866) with full attribute support
- F-002: Diameter base protocol (RFC 6733) with 3GPP Gx (policy) and Gy (charging) applications
- F-003: 5G SBA proxy — HTTP/2 interface for AUSF/UDM integration
- F-004: EAP-SIM, EAP-AKA, EAP-AKA' authentication methods
- F-005: Network slice authentication for 5G SA networks
- F-006: CoA/DM — real-time session modification and disconnect
- F-007: Session management — configurable max concurrent sessions per SIM (default: 1)
- F-008: Active-active HA clustering
- F-009: RadSec (RADIUS/TLS) and Diameter/TLS
- F-010: Protocol resilience — retry, circuit breaker per operator, dead letter queue

#### SIM & APN Lifecycle
- F-011: SIM provisioning — single and bulk import with auto-activate
- F-012: SIM state machine — ORDERED → ACTIVE ↔ SUSPENDED → TERMINATED → PURGED + STOLEN/LOST
- F-013: eSIM management — SM-DP+ API integration, cross-operator profile switch, bulk eSIM provisioning
- F-014: APN CRUD — create/modify/delete with ARCHIVED soft-delete, hard block on active SIMs
- F-015: IPAM — per-APN/per-operator pools, static reservation, conflict detection, IPv4+IPv6 dual-stack, utilization alerts (80/90/100%), configurable reclaim grace period
- F-016: IMSI/MSISDN/ICCID inventory with MSISDN number pooling
- F-017: OTA SIM management via APDU commands
- F-018: Bulk operations — async job queue, progress bar, partial success, retry failed, error report CSV, undo/rollback
- F-019: Configurable KVKK/GDPR purge retention period with auto-purge

#### Multi-Operator Orchestration
- F-020: Pluggable operator adapter framework with mock simulator
- F-021: IMSI-prefix based intelligent routing
- F-022: Steering of Roaming (SoR) engine with RAT-type preference
- F-023: Configurable operator failover — reject / fallback-to-next / queue-with-timeout
- F-024: Operator health check heartbeat + SLA violation events
- F-025: Diameter ↔ RADIUS protocol bridge
- F-026: RAT-type awareness across all layers (NB-IoT, LTE-M, 4G, 5G)

#### Policy & Charging (mini-PCRF)
- F-027: QoS enforcement — bandwidth limit per APN/subscriber/RAT-type
- F-028: Dynamic policy rules — time-of-day, location, quota, RAT-type conditions
- F-029: Charging rules — prepaid/postpaid, quota management
- F-030: FUP enforcement
- F-031: Slice-aware policy rules
- F-032: Policy DSL / rule engine — per-operator configurable
- F-033: Policy versioning with rollback
- F-034: Dry-run simulation — "this rule affects N SIMs" preview
- F-035: Staged rollout — canary 1% → 10% → 100%, concurrent policy versions, CoA on each stage

#### BI & Analytics
- F-036: Real-time usage dashboards — per SIM/APN/operator/RAT-type
- F-037: Anomaly detection — SIM cloning, abuse patterns, data spikes
- F-038: Cost optimization engine — cheapest operator routing recommendations
- F-039: CDR processing & rating engine with carrier cost tracking
- F-040: RAT-type cost differentiation
- F-041: Compliance reporting — BTK, KVKK, GDPR report generation
- F-042: Built-in observability — auth/s, latency percentiles, error rate, session count dashboards

#### Portal
- F-043: Tenant dashboard — system health, SIM summary, alert feed, active sessions, top APNs, quick actions
- F-044: Group-first SIM management — segments, saved filters, bulk actions; individual SIM as drill-down
- F-045: SIM detail page — state history, session history, usage chart, policy, APN, operator, eSIM profile
- F-046: SIM combo search — IMSI/MSISDN/ICCID/IP/APN/operator/state
- F-047: Connectivity diagnostics — auto-diagnosis, connectivity test, troubleshooting wizard
- F-048: Dark mode default + light mode toggle, premium visual design (frontend-design skill)
- F-049: Command palette (Ctrl+K) — quick navigation
- F-050: Contextual error messages with suggested actions
- F-051: Undo capability for state changes, policy assignments, bulk ops
- F-052: Notification center (bell icon, read/unread)

#### API & Integration
- F-053: REST API — all operations API-first
- F-054: Event streaming — WebSocket/SSE for real-time data
- F-055: SMS Gateway — outbound for IoT device management
- F-056: API key management — create, rotate, rate limit, scope restrict, revoke
- F-057: OAuth2 client credentials for third-party integration
- F-058: Webhook delivery for notifications

#### Platform & Security
- F-059: Multi-tenant architecture — tenant_id on every table
- F-060: RBAC — Super Admin, Tenant Admin, Operator Manager, SIM Manager, Policy Editor, Analyst, API User
- F-061: Tenant onboarding wizard — create tenant → invite admin → connect operators → define APNs → import SIMs → assign policies
- F-062: Resource limits per tenant (max SIM, APN, users)
- F-063: JWT + refresh token + 2FA (TOTP) for portal auth
- F-064: Deep audit log — tamper-proof hash chain, before/after diff, searchable, exportable
- F-065: Pseudonymization on KVKK/GDPR purge (audit log integrity preserved)
- F-066: Configurable rate limiting — per-tenant, per-API-key, per-endpoint
- F-067: Notification channels — in-app, email, webhook, Telegram; scopes — per-SIM, per-APN, per-operator, system-wide (percentage-based thresholds)
- F-068: Background job system — NATS queue, job dashboard, distributed lock, scheduled jobs (cron)
- F-069: TLS everywhere — HTTPS, RadSec, Diameter/TLS
- F-070: Input validation/sanitization, CORS per-tenant

### Should Have (v1 but lower priority within development phases)
- F-071: SIM comparison — side-by-side debug view
- F-072: Roaming agreement management UI
### Won't Have (explicitly excluded)
- Predictive analytics / ML-based predictions (deferred to FUTURE.md FTR-002: AI & Predictive Intelligence)
- Own SM-DP+ server
- SGP.32 eIM support
- White-label / custom branding per tenant
- VoWiFi / WiFi Offload AAA
- TACACS+ protocol
- Geo-fencing
- Device management (firmware, health monitoring)

## User Workflows

### WF-1: SIM Bulk Provisioning
```
Tenant Admin uploads CSV (ICCID, IMSI, MSISDN, operator, APN)
  → System validates CSV format + uniqueness checks
  → Background job created → progress bar in portal
  → Per-row: create SIM record (ORDERED) → auto-activate (ACTIVE) → assign APN → assign default policy → allocate IP from pool
  → Partial success: successful rows applied, failed rows in error report CSV
  → Notification on completion (in-app + configured channels)
  → Tenant Admin can retry failed rows or download error report
```

### WF-2: Policy Staged Rollout
```
Policy Editor creates new policy version in DSL editor
  → Dry-run: "This policy affects 2.3M SIMs across 4 APNs"
  → Policy Editor selects staged rollout (1% → 10% → 100%)
  → Stage 1 (1%): 23K SIMs get new policy version + CoA sent
  → Dashboard shows rollout progress + impact metrics
  → Policy Editor reviews metrics → approves next stage
  → Stage 2 (10%): 230K SIMs migrated + CoA
  → Stage 3 (100%): all 2.3M SIMs migrated
  → At any point: rollback → all SIMs revert to previous version + CoA
```

### WF-3: Operator Failover
```
Turkcell RADIUS/Diameter connection drops
  → Circuit breaker triggers after N consecutive failures
  → Operator marked DEGRADED → alert sent (in-app + email + Telegram)
  → Per SIM failover policy applied:
    - "reject": auth requests rejected, session denied
    - "fallback-to-next": route to Vodafone adapter
    - "queue-with-timeout": hold for N seconds, then fallback or reject
  → SLA violation event logged → analytics
  → When Turkcell recovers → circuit breaker resets → traffic restored
  → SLA report generated with downtime duration + affected SIM count
```

### WF-4: eSIM Cross-Operator Switch
```
SIM Manager selects SIM segment "Fleet APN - Turkcell"
  → Bulk action: "Switch to Vodafone"
  → System checks: Vodafone adapter connected, APN available, IP pool capacity
  → Background job: per-SIM → call Turkcell SM-DP+ API (disable profile) → call Vodafone SM-DP+ API (enable profile) → update SIM record → reassign policy → reallocate IP
  → Progress bar + partial success handling
  → CoA sent to active sessions for policy update
  → Analytics: cost comparison before/after operator switch
```

### WF-5: Connectivity Diagnostics
```
SIM Manager sees SIM in ACTIVE state but device not connecting
  → Click "Diagnose" on SIM detail page
  → Auto-diagnosis runs:
    1. Check last auth attempt → found: rejected 5 min ago
    2. Check reject reason → "APN not found"
    3. Check APN config → APN "iot.meter" exists but not mapped to operator
    4. Suggested action: "Map APN 'iot.meter' to Turkcell" [Fix Now] button
  → SIM Manager clicks [Fix Now] → APN mapped → triggers re-auth test
  → Connectivity test: "Auth successful, session established, IP assigned"
```

### WF-6: Tenant Onboarding
```
Super Admin → Create Tenant (company name, domain, contact)
  → Tenant Admin user auto-created → invite email sent
  → Tenant Admin logs in → Onboarding Wizard:
    Step 1: Connect operators (select from system-level adapters, request access)
    Step 2: Define APNs (name, type, operator, IP pool)
    Step 3: Import first SIM batch (CSV upload)
    Step 4: Assign default policy to SIM segment
    Step 5: Configure notification preferences
  → Dashboard populated → tenant operational
```

## Business Rules

### BR-1: SIM State Transitions
| From | To | Trigger | Authorization | Side Effects |
|------|----|---------|--------------|-------------|
| ORDERED | ACTIVE | Bulk import (auto) or manual activate | SIM Manager, Tenant Admin | Allocate IP, apply default policy |
| ACTIVE | SUSPENDED | Manual or policy trigger (quota exceeded) | SIM Manager, Tenant Admin, Policy Engine | CoA/DM to kill session, retain IP |
| SUSPENDED | ACTIVE | Manual resume | SIM Manager, Tenant Admin | Re-establish session eligibility |
| ACTIVE | STOLEN/LOST | Report stolen/lost | SIM Manager, Tenant Admin | Immediate CoA/DM, flag in analytics |
| ACTIVE | TERMINATED | Manual or bulk terminate | Tenant Admin | CoA/DM, release IP (after grace period), stop billing |
| SUSPENDED | TERMINATED | Manual terminate | Tenant Admin | Release IP (after grace period) |
| STOLEN/LOST | TERMINATED | Manual terminate after investigation | Tenant Admin | Release IP |
| TERMINATED | PURGED | Auto-purge after configurable retention days | System (scheduled job) | Pseudonymize audit logs, delete personal data |

### BR-2: APN Deletion Rules
- APN with active SIMs → DELETE blocked (hard constraint)
- APN with no active SIMs → soft-delete to ARCHIVED state
- ARCHIVED APN: no new SIM assignment, existing data retained for audit
- Permanent delete only after all related SIMs are TERMINATED + PURGED

### BR-3: IP Address Management
- IP allocated on SIM activation, from APN's assigned pool
- Static IP: reserved per-SIM, never returned to pool while SIM exists
- Dynamic IP: returned to pool on session end
- On SIM termination: IP held for configurable grace period, then reclaimed
- Pool at 80%: warning alert. 90%: critical alert. 100%: new allocations rejected + alert

### BR-4: Policy Enforcement
- Policy changes propagated via CoA to active sessions
- Staged rollout: SIMs track their assigned policy version
- Multiple policy versions can coexist during rollout
- Rollback: mass CoA to revert all SIMs to previous version
- Policy evaluation order: SIM-specific > APN-level > operator-level > tenant default

### BR-5: Operator Failover
- Health check: configurable heartbeat interval per operator
- Circuit breaker: configurable threshold (N consecutive failures) and recovery window
- Failover policy: per-operator configurable (reject / fallback / queue)
- SLA violation: logged as event, triggers notification, visible in analytics

### BR-6: Tenant Isolation
- All data queries scoped by tenant_id (enforced at ORM/middleware level)
- Operator adapters: system-level (shared), tenant gets access grants
- Resource limits: max SIMs, APNs, users per tenant (configurable by Super Admin)
- Cross-tenant data access: impossible by design

### BR-7: Audit & Compliance
- Every state-changing operation logged with: who, when, what, before/after diff
- Audit log: append-only, hash chain for tamper detection
- On KVKK/GDPR purge: personal identifiers pseudonymized (IMSI→hash, MSISDN→hash), mapping table deleted
- Audit log retention: independent of data retention, configurable separately
- Failed login attempts logged and rate-limited

## Non-Functional Requirements

### Performance
| Metric | Requirement |
|--------|------------|
| Auth throughput | 10K+ requests/second per node |
| Auth latency | p50 <5ms, p95 <20ms, p99 <50ms |
| Portal page load | <500ms with data |
| Bulk operation | 10K+ SIMs per batch, async processing |
| WebSocket updates | <100ms event-to-UI latency |
| Database | 10M+ SIM records, cursor-based pagination |

### Security
- JWT + refresh token + 2FA (TOTP) for portal
- API key + OAuth2 client credentials for API
- Configurable rate limiting (per-tenant, per-key, per-endpoint)
- TLS everywhere (HTTPS, RadSec, Diameter/TLS)
- Input validation, XSS/SQLI prevention, CORS per-tenant
- Credential security (.env, encrypted at rest, masked in API responses)

### Compliance
- BTK: local operator integration, data localization awareness
- KVKK: personal data retention limits, auto-purge, pseudonymization
- GDPR: right to erasure, data portability, consent tracking
- ISO 27001: audit trail, access control, incident logging

### Availability
- AAA core: 99.9% uptime target
- Active-active clustering, no single point of failure
- Graceful degradation: portal can operate in read-only if AAA core is under maintenance

### Scalability
- Horizontal scaling: add AAA nodes behind load balancer
- Database: partitioning, read replicas, TimescaleDB compression
- Redis: session cache, policy cache with NATS invalidation
- NATS: event bus, job queue

## Integration Points

| System | Protocol | Direction | Purpose |
|--------|----------|-----------|---------|
| Operator RADIUS | RADIUS/UDP, RadSec/TLS | Bidirectional | Authentication, authorization, accounting |
| Operator Diameter | Diameter/TCP/TLS | Bidirectional | Gx (policy), Gy (charging) |
| Operator 5G Core | HTTP/2 | Bidirectional | AUSF/UDM proxy for 5G SA |
| Operator SM-DP+ | HTTPS REST API | Outbound | eSIM profile provisioning, switch, delete |
| Email (SMTP) | SMTP | Outbound | Notifications, invitations |
| Telegram Bot API | HTTPS | Outbound | Notifications |
| S3-compatible storage | HTTPS | Outbound | Audit archive, bulk export, cold storage |
| Webhook endpoints | HTTPS | Outbound | Event notifications to external systems |
| SMS Gateway | HTTPS/SMPP | Outbound | Device management commands |

## Data Model Overview

### Core Entities

```
Tenant ──1:N──▶ User (with Role)
Tenant ──1:N──▶ APN ──1:N──▶ SIM
Tenant ──1:N──▶ Policy ──1:N──▶ PolicyVersion
Tenant ──1:N──▶ IPPool ──1:N──▶ IPAddress

SIM ──N:1──▶ Operator
SIM ──N:1──▶ APN
SIM ──N:1──▶ PolicyVersion (assigned)
SIM ──1:N──▶ Session
SIM ──1:N──▶ SIMStateHistory
SIM ──0:1──▶ IPAddress (static reservation)
SIM ──0:1──▶ eSIMProfile

Operator ──1:N──▶ OperatorAdapter (system-level)
Operator ──N:N──▶ Tenant (access grants)
Operator ──1:N──▶ APN

Session ──N:1──▶ SIM
Session ──N:1──▶ Operator
Session ──1:N──▶ AccountingRecord (CDR)

AuditLog (append-only, hash chain)
Job (background task queue)
Notification
APIKey ──N:1──▶ Tenant
```

### Key Tables (simplified)

| Entity | Key Fields | Notes |
|--------|-----------|-------|
| Tenant | id, name, domain, resource_limits, created_at | Root isolation entity |
| User | id, tenant_id, email, role, 2fa_enabled | RBAC role enum |
| Operator | id, name, adapter_type, health_status, failover_policy | System-level |
| OperatorGrant | tenant_id, operator_id, enabled | Tenant ↔ Operator access |
| APN | id, tenant_id, operator_id, name, state (ACTIVE/ARCHIVED), rat_types | Soft-delete |
| SIM | id, tenant_id, iccid, imsi, msisdn, operator_id, apn_id, policy_version_id, state, ip_address_id | Partitioned by operator/state |
| eSIMProfile | id, sim_id, eid, profile_state, sm_dp_plus_id | SM-DP+ reference |
| Policy | id, tenant_id, name, scope (APN/operator/global) | Container for versions |
| PolicyVersion | id, policy_id, version, dsl_content, state (DRAFT/ACTIVE/ROLLING_OUT/ROLLED_BACK) | Versioned rules |
| IPPool | id, tenant_id, apn_id, cidr_v4, cidr_v6, utilization_pct | Dual-stack |
| IPAddress | id, pool_id, address, type (STATIC/DYNAMIC), sim_id, state | Conflict detection |
| Session | id, sim_id, operator_id, started_at, ended_at, ip_address, rat_type | Active session tracking |
| CDR | id, session_id, usage_bytes, duration, rat_type, cost, timestamp | TimescaleDB hypertable |
| AuditLog | id, tenant_id, user_id, action, entity_type, entity_id, before, after, hash, prev_hash, created_at | Append-only, partitioned by date |
| Job | id, tenant_id, type, state, progress_pct, error_report, created_at | NATS-backed |
| Notification | id, tenant_id, user_id, channel, scope, event_type, state (UNREAD/READ), created_at | Multi-channel |
| APIKey | id, tenant_id, key_hash, scopes, rate_limit, expires_at, revoked_at | Never store plaintext |
