# User Acceptance Test Scenarios

> Generated: 2026-03-23
> Total scenarios: 23
> Story coverage: 55/55 stories (100%)
> Business rule coverage: 7/7 rules (100%)
> Screen coverage: 28/28 screens (100%)

---

## UAT-001: Tenant Onboarding → First Dashboard

**Business Context**: New tenant must go from zero to operational in a single guided wizard flow. Validates the complete onboarding pipeline end-to-end.
**Trigger**: Super Admin creates a new tenant
**Roles Involved**: Super Admin, Tenant Admin
**Business Rules**: BR-6 (Tenant Isolation), BR-7 (Audit)
**Stories**: STORY-001, STORY-002, STORY-003, STORY-005, STORY-009, STORY-010, STORY-011, STORY-013, STORY-022, STORY-038, STORY-039

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Super Admin | SCR-121: Tenant Management | Create new tenant (company name, domain, contact, resource limits) | Tenant record created, admin user auto-created |
| 2 | System | Email Service | — | Invite email sent to Tenant Admin with credentials |
| 3 | Tenant Admin | SCR-001: Login | Login with credentials from invite email | Redirected to SCR-003: Onboarding Wizard |
| 4 | Tenant Admin | SCR-003: Onboarding Wizard | Step 1: Connect operators (select MockTurkcell, MockVodafone) | Operator grants created, adapters initialized |
| 5 | Tenant Admin | SCR-003: Onboarding Wizard | Step 2: Define APNs (name: "iot.fleet", type: private, operator: MockTurkcell) | APN created with ACTIVE state |
| 6 | Tenant Admin | SCR-003: Onboarding Wizard | Step 3: Upload first SIM batch (CSV with 100 SIMs) | Background job created, progress bar shown |
| 7 | System | Job Runner (SVC-09) | — | SIMs created (ORDERED→ACTIVE), IPs allocated, default policy assigned |
| 8 | Tenant Admin | SCR-003: Onboarding Wizard | Step 4: Assign default policy to SIM segment | Policy version linked to all imported SIMs |
| 9 | Tenant Admin | SCR-003: Onboarding Wizard | Step 5: Configure notification preferences (in-app + webhook) | Notification config saved |
| 10 | Tenant Admin | SCR-010: Main Dashboard | Wizard completes, redirected to dashboard | Dashboard shows: 100 SIMs, 1 APN, 2 operators, system health OK |

### Verify (Post-Flow Checks)

- [ ] Tenant record exists in DB with correct resource limits
- [ ] Tenant Admin user has `tenant_admin` role
- [ ] Operator grants exist for both MockTurkcell and MockVodafone
- [ ] APN "iot.fleet" is ACTIVE and scoped to tenant
- [ ] 100 SIM records exist with state ACTIVE, IP allocated, policy assigned
- [ ] Dashboard widget counts match (SIMs: 100, APNs: 1, Operators: 2)
- [ ] Audit log contains: tenant_created, user_created, operator_grant_created, apn_created, sim_bulk_import events
- [ ] All data is scoped by tenant_id (no cross-tenant leakage)

---

## UAT-002: SIM Bulk Import → Dashboard Reflection

**Business Context**: Enterprises need to onboard thousands of SIMs at once. The system must process CSV imports asynchronously with full visibility into progress and results.
**Trigger**: Tenant Admin uploads a CSV file with SIM data
**Roles Involved**: Tenant Admin, SIM Manager
**Business Rules**: BR-1 (SIM State Transitions), BR-3 (IP Management), BR-7 (Audit)
**Stories**: STORY-013, STORY-011, STORY-010, STORY-031, STORY-038, STORY-039, STORY-007

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-020: SIM List | Click "Import SIMs" → upload CSV (500 SIMs: ICCID, IMSI, MSISDN, operator, APN) | CSV validated, background job created |
| 2 | Tenant Admin | SCR-080: Job List | Navigate to jobs | Import job visible with progress bar (0%) |
| 3 | System | Job Runner (SVC-09) | — | Per-row: create SIM (ORDERED→ACTIVE), assign APN, allocate IP from pool, assign default policy |
| 4 | Tenant Admin | SCR-080: Job List | Refresh | Progress bar updates (50%... 100%) |
| 5 | System | Notification (SVC-08) | — | "Bulk import complete: 495 success, 5 failed" notification |
| 6 | Tenant Admin | SCR-100: Notifications | Check notification bell | Import completion notification with success/fail counts |
| 7 | Tenant Admin | SCR-080: Job List | Click import job → download error report | CSV with 5 failed rows and error reasons (e.g., duplicate ICCID) |
| 8 | Tenant Admin | SCR-020: SIM List | Navigate to SIM list | 495 new SIMs visible, filterable by import batch |
| 9 | Tenant Admin | SCR-010: Main Dashboard | Check dashboard | SIM count widget incremented by 495 |
| 10 | Tenant Admin | SCR-090: Audit Log | Search audit log | 495 sim_created + 495 sim_activated entries with before/after diffs |

### Verify (Post-Flow Checks)

- [ ] 495 SIM records with state ACTIVE, each with allocated IP and assigned policy
- [ ] 5 failed rows have clear error reasons in error report CSV
- [ ] IP pool utilization percentage updated correctly
- [ ] Dashboard SIM count, APN SIM count both reflect new totals
- [ ] Job record shows final status "completed" with progress 100%
- [ ] Audit log hash chain integrity maintained across all 990+ entries

---

## UAT-003: SIM Full Lifecycle (State Machine)

**Business Context**: SIM cards go through a complete lifecycle from provisioning to purge. Each state transition must enforce business rules, update dependent systems, and maintain full audit trail.
**Trigger**: SIM Manager activates a new SIM
**Roles Involved**: SIM Manager, Tenant Admin, System (scheduled job)
**Business Rules**: BR-1 (SIM State Transitions), BR-3 (IP Management), BR-4 (Policy), BR-7 (Audit)
**Stories**: STORY-011, STORY-017, STORY-015, STORY-007, STORY-044

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | SIM Manager | SCR-020: SIM List | Create single SIM (ICCID, IMSI, MSISDN, operator, APN) | SIM created in ORDERED state |
| 2 | SIM Manager | SCR-021: SIM Detail | Click "Activate" | SIM transitions to ACTIVE, IP allocated, default policy assigned |
| 3 | System | AAA Engine (SVC-04) | SIM authenticates via RADIUS | Session created, visible in SCR-050 |
| 4 | SIM Manager | SCR-021: SIM Detail | Click "Suspend" with reason "Quota exceeded" | SIM → SUSPENDED, CoA/DM sent, session terminated |
| 5 | SIM Manager | SCR-050: Live Sessions | Check sessions | Session for this SIM no longer active |
| 6 | SIM Manager | SCR-021: SIM Detail | Click "Resume" | SIM → ACTIVE, session eligibility restored, IP retained |
| 7 | SIM Manager | SCR-021: SIM Detail | Click "Report Stolen/Lost" | SIM → STOLEN/LOST, immediate CoA/DM, analytics flag |
| 8 | Tenant Admin | SCR-021: SIM Detail | Click "Terminate" | SIM → TERMINATED, IP enters grace period, billing stopped |
| 9 | System | Scheduled Job (SVC-09) | Auto-purge after retention period | SIM → PURGED, personal data pseudonymized |
| 10 | Tenant Admin | SCR-021e: SIM History | View state history tab | Full timeline: ORDERED→ACTIVE→SUSPENDED→ACTIVE→STOLEN/LOST→TERMINATED→PURGED |
| 11 | Tenant Admin | SCR-090: Audit Log | Search by SIM ICCID | All state transitions logged with actor, timestamp, before/after |

### Verify (Post-Flow Checks)

- [ ] Each state transition recorded in sim_state_history table
- [ ] IP allocated on ACTIVE, retained on SUSPEND, grace period on TERMINATE, reclaimed on PURGE
- [ ] CoA/DM sent on SUSPEND, STOLEN/LOST, TERMINATE
- [ ] Policy assignment cleared on TERMINATE
- [ ] After PURGE: IMSI→hash, MSISDN→hash in audit logs (pseudonymization)
- [ ] Audit log hash chain valid across all transitions
- [ ] Invalid transitions rejected (e.g., ORDERED→SUSPENDED returns error)

---

## UAT-004: Policy Staged Rollout

**Business Context**: Policy changes affecting millions of SIMs must be rolled out safely with dry-run preview, staged canary deployment, and instant rollback capability.
**Trigger**: Policy Editor creates a new policy version
**Roles Involved**: Policy Editor
**Business Rules**: BR-4 (Policy Enforcement)
**Stories**: STORY-022, STORY-023, STORY-024, STORY-025, STORY-017

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Policy Editor | SCR-060: Policy List | View existing policies | List of policies with version counts |
| 2 | Policy Editor | SCR-062: Policy Editor | Create new policy version in DSL editor (e.g., bandwidth limit 10Mbps for LTE) | DSL parsed, version saved as DRAFT |
| 3 | Policy Editor | SCR-062: Policy Editor | Click "Dry Run" | Preview: "This policy affects 200 SIMs across 2 APNs" with affected SIM list |
| 4 | Policy Editor | SCR-062: Policy Editor | Select "Staged Rollout" → Start Stage 1 (1%) | 2 SIMs get new policy version, CoA sent |
| 5 | System | AAA Engine (SVC-04) | — | CoA messages sent to 2 active sessions, QoS updated |
| 6 | Policy Editor | SCR-062: Policy Editor | View rollout dashboard | Stage 1: 2/200 SIMs (1%), impact metrics displayed |
| 7 | Policy Editor | SCR-062: Policy Editor | Approve → Stage 2 (10%) | 20 SIMs migrated, CoA sent to active sessions |
| 8 | Policy Editor | SCR-062: Policy Editor | Approve → Stage 3 (100%) | All 200 SIMs migrated, policy version state → ACTIVE |
| 9 | Policy Editor | SCR-020: SIM List | Filter by policy version | All 200 SIMs show new policy version |
| 10 | Policy Editor | SCR-062: Policy Editor | Click "Rollback" | All 200 SIMs revert to previous version, mass CoA sent |
| 11 | Policy Editor | SCR-090: Audit Log | View audit entries | rollout_started, stage_advanced (×3), rollback events logged |

### Verify (Post-Flow Checks)

- [ ] Dry-run count matches actual affected SIMs
- [ ] During rollout: two policy versions coexist (old + new)
- [ ] CoA sent at each stage transition to active sessions
- [ ] After rollback: all SIMs back on previous version
- [ ] Policy version states: DRAFT→ROLLING_OUT→ROLLED_BACK
- [ ] SCR-050 sessions reflect updated QoS parameters during rollout
- [ ] Audit log captures every stage transition with SIM counts

---

## UAT-005: Operator Failover & Recovery

**Business Context**: When an operator connection degrades, the system must automatically detect failure, trigger circuit breaker, apply failover policy, and recover when the operator comes back online.
**Trigger**: Operator health check fails consecutively
**Roles Involved**: System (automatic), Operator Manager
**Business Rules**: BR-5 (Operator Failover)
**Stories**: STORY-021, STORY-018, STORY-009, STORY-033, STORY-038

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | Operator Router (SVC-06) | MockTurkcell health check starts failing (mock config: success_rate=0) | Consecutive failure counter increments |
| 2 | System | Circuit Breaker | Failure count exceeds threshold (default: 5) | Circuit breaker opens, operator marked DEGRADED |
| 3 | System | Notification (SVC-08) | — | Alert sent: "Operator Turkcell DEGRADED" (in-app + email + Telegram) |
| 4 | Operator Manager | SCR-040: Operator List | View operator list | Turkcell shows DEGRADED status badge |
| 5 | Operator Manager | SCR-041: Operator Detail | View Turkcell detail | Health timeline shows failure events, circuit breaker state: OPEN |
| 6 | System | AAA Engine (SVC-04) | New auth request for Turkcell SIM arrives | Failover policy applied (e.g., fallback-to-next → routes to Vodafone) |
| 7 | Operator Manager | SCR-010: Main Dashboard | Check dashboard | Alert feed shows operator degradation, affected SIM count |
| 8 | Operator Manager | SCR-013: Anomalies | View anomaly dashboard | SLA violation event visible with downtime duration |
| 9 | System | Circuit Breaker | Recovery window elapsed, mock config updated: success_rate=100 | Circuit breaker → HALF-OPEN, test request sent |
| 10 | System | Circuit Breaker | Test request succeeds | Circuit breaker → CLOSED, operator → ACTIVE |
| 11 | System | Notification (SVC-08) | — | "Operator Turkcell recovered" notification |
| 12 | Operator Manager | SCR-041: Operator Detail | View detail | Health timeline shows recovery, SLA report with downtime duration + affected SIM count |

### Verify (Post-Flow Checks)

- [ ] Circuit breaker transitions: CLOSED→OPEN→HALF-OPEN→CLOSED logged
- [ ] During OPEN: auth requests routed via failover policy (not rejected silently)
- [ ] SLA violation event recorded with exact downtime duration
- [ ] Notification delivered on both degradation and recovery
- [ ] Dashboard alert feed updated in real-time via WebSocket
- [ ] Audit log: operator_health_changed events with before/after states

---

## UAT-006: eSIM Cross-Operator Switch

**Business Context**: Enterprise fleet managers need to switch thousands of eSIM profiles from one operator to another for cost optimization or coverage improvement.
**Trigger**: SIM Manager initiates bulk operator switch on a SIM segment
**Roles Involved**: SIM Manager
**Business Rules**: BR-1 (SIM State), BR-3 (IP Management), BR-4 (Policy)
**Stories**: STORY-028, STORY-030, STORY-012, STORY-031, STORY-018

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | SIM Manager | SCR-020: SIM List | Create segment "Fleet APN - Turkcell" (filter: operator=Turkcell, APN=iot.fleet) | Segment saved with 50 matching SIMs |
| 2 | SIM Manager | SCR-020: SIM List | Select segment → Bulk action: "Switch to Vodafone" | Pre-check: Vodafone adapter connected, APN available, IP pool capacity |
| 3 | System | Job Runner (SVC-09) | Background job created | Job visible in SCR-080 |
| 4 | System | eSIM Service | Per-SIM: MockSMDPAdapter.DisableProfile(Turkcell) → MockSMDPAdapter.EnableProfile(Vodafone) | Profile switch atomic per SIM |
| 5 | System | Core API (SVC-03) | Per-SIM: update operator_id, reassign APN, reallocate IP, reassign policy | SIM record updated |
| 6 | System | AAA Engine (SVC-04) | CoA sent to active sessions for policy update | Active sessions updated |
| 7 | SIM Manager | SCR-080: Job List | Monitor progress | Progress bar: 0%→50%→100% |
| 8 | SIM Manager | SCR-070: eSIM Profiles | View eSIM profile list | 50 SIMs now show Vodafone profile as enabled |
| 9 | SIM Manager | SCR-021: SIM Detail | Open any switched SIM | Operator: Vodafone, new IP, new policy version |
| 10 | SIM Manager | SCR-090: Audit Log | Search by operation | 50× profile_disabled, 50× profile_enabled, 50× sim_operator_changed |

### Verify (Post-Flow Checks)

- [ ] All 50 SIMs have operator_id=Vodafone
- [ ] Old Turkcell profiles in "disabled" state, new Vodafone profiles in "enabled" state
- [ ] Only one profile per SIM in "enabled" state (enforced by DB constraint)
- [ ] IPs reallocated from Vodafone pool
- [ ] Policy reassigned for new operator context
- [ ] Job record shows success count, any partial failures with reasons

---

## UAT-007: Connectivity Diagnostics

**Business Context**: When a device can't connect despite its SIM being ACTIVE, the diagnostics wizard must automatically identify the root cause and offer one-click fixes.
**Trigger**: SIM Manager sees ACTIVE SIM but device not connecting
**Roles Involved**: SIM Manager
**Business Rules**: BR-3 (IP), BR-4 (Policy)
**Stories**: STORY-037, STORY-011, STORY-010, STORY-015

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | SIM Manager | SCR-020: SIM List | Find SIM with ACTIVE state, no active session | SIM identified |
| 2 | SIM Manager | SCR-021: SIM Detail | Note: state=ACTIVE but "No active session" badge | Indicates connectivity issue |
| 3 | SIM Manager | SCR-021d: SIM Diagnostics | Click "Diagnose" | Auto-diagnosis starts |
| 4 | System | Diagnostics Engine | Step 1: Check last auth attempt | Found: rejected 5 min ago |
| 5 | System | Diagnostics Engine | Step 2: Check reject reason | "APN not found" |
| 6 | System | Diagnostics Engine | Step 3: Check APN config | APN "iot.meter" exists but not mapped to operator |
| 7 | System | Diagnostics Engine | Step 4: Suggest fix | "Map APN 'iot.meter' to Turkcell" with [Fix Now] button |
| 8 | SIM Manager | SCR-021d: SIM Diagnostics | Click [Fix Now] | APN mapped to operator, re-auth test triggered |
| 9 | System | AAA Engine (SVC-04) | Re-auth test via MockAdapter | "Auth successful, session established, IP assigned" |
| 10 | SIM Manager | SCR-021: SIM Detail | View overview | Active session badge shown, IP address displayed |

### Verify (Post-Flow Checks)

- [ ] Diagnostics log shows step-by-step investigation trail
- [ ] APN-operator mapping created in DB
- [ ] New session record exists after fix
- [ ] Audit log: apn_operator_mapped, diagnostic_fix_applied events
- [ ] SCR-050 shows new active session for this SIM

---

## UAT-008: APN Deletion Guard

**Business Context**: APNs with active SIMs must never be deleted to prevent connectivity loss. The system enforces a strict lifecycle: block delete → move SIMs → archive → permanent delete only after all SIMs purged.
**Trigger**: Admin attempts to delete an APN
**Roles Involved**: SIM Manager, Tenant Admin
**Business Rules**: BR-2 (APN Deletion Rules)
**Stories**: STORY-010, STORY-011

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | SIM Manager | SCR-030: APN List | View APN list with "iot.fleet" (10 active SIMs) | APN shown with SIM count badge |
| 2 | SIM Manager | SCR-032: APN Detail | Click "Delete" on "iot.fleet" | Error: "Cannot delete APN with 10 active SIMs" (hard block) |
| 3 | SIM Manager | SCR-020: SIM List | Move all 10 SIMs to different APN | SIMs reassigned successfully |
| 4 | SIM Manager | SCR-032: APN Detail | Click "Delete" on "iot.fleet" (now 0 active SIMs) | APN transitions to ARCHIVED state (soft-delete) |
| 5 | SIM Manager | SCR-030: APN List | View APN list | "iot.fleet" shown with ARCHIVED badge |
| 6 | SIM Manager | SCR-020: SIM List | Try to assign new SIM to archived APN | Error: "Cannot assign SIM to archived APN" |
| 7 | Tenant Admin | SCR-090: Audit Log | View audit | apn_delete_blocked, sims_reassigned, apn_archived events |

### Verify (Post-Flow Checks)

- [ ] APN state is ARCHIVED in DB
- [ ] No new SIM assignments possible to ARCHIVED APN
- [ ] Existing audit/CDR data referencing this APN retained
- [ ] Permanent delete blocked until all related SIMs TERMINATED + PURGED

---

## UAT-009: IP Pool Exhaustion Alert

**Business Context**: IP pool capacity must be monitored with threshold alerts (80%/90%/100%) to prevent allocation failures that would block SIM activations.
**Trigger**: IP pool utilization crosses alert thresholds
**Roles Involved**: Operator Manager, SIM Manager
**Business Rules**: BR-3 (IP Address Management)
**Stories**: STORY-010, STORY-011, STORY-038, STORY-039

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Operator Manager | SCR-112: IP Pools | Create small IP pool (10 IPs: 10.0.0.1-10.0.0.10) for APN "test.pool" | Pool created, utilization 0% |
| 2 | SIM Manager | SCR-020: SIM List | Activate 8 SIMs on "test.pool" APN | 8 IPs allocated, utilization 80% |
| 3 | System | Notification (SVC-08) | — | Warning alert: "IP Pool 'test.pool' at 80% capacity" |
| 4 | SIM Manager | SCR-100: Notifications | Check notifications | 80% warning notification visible |
| 5 | SIM Manager | SCR-020: SIM List | Activate 1 more SIM | 9 IPs allocated, utilization 90% |
| 6 | System | Notification (SVC-08) | — | Critical alert: "IP Pool 'test.pool' at 90% capacity" |
| 7 | SIM Manager | SCR-020: SIM List | Activate 1 more SIM | 10 IPs allocated, utilization 100% |
| 8 | System | Notification (SVC-08) | — | Critical alert: "IP Pool 'test.pool' FULL — new allocations will fail" |
| 9 | SIM Manager | SCR-020: SIM List | Try to activate 11th SIM | Error: "IP pool exhausted — no available addresses" |
| 10 | Operator Manager | SCR-010: Main Dashboard | Check dashboard | IP pool alert visible in alert feed |
| 11 | Operator Manager | SCR-112: IP Pools | View pool detail | Utilization bar at 100%, per-IP allocation table |

### Verify (Post-Flow Checks)

- [ ] Alerts fired at exactly 80%, 90%, 100% thresholds
- [ ] 11th SIM activation blocked (not silently failed)
- [ ] Pool utilization percentage accurate in DB
- [ ] Dashboard reflects pool status in real-time
- [ ] Releasing a SIM's IP (terminate SIM) reduces utilization and re-enables allocation

---

## UAT-010: CDR → Cost Analytics → Anomaly Detection

**Business Context**: Session data must flow through CDR processing, feed cost analytics dashboards, and trigger anomaly detection when unusual patterns are detected.
**Trigger**: Active sessions generate accounting data
**Roles Involved**: System (automatic), Analyst
**Business Rules**: BR-7 (Audit)
**Stories**: STORY-032, STORY-034, STORY-035, STORY-036, STORY-033

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | AAA Engine (SVC-04) | Multiple SIMs authenticate, sessions active | Sessions visible in SCR-050 |
| 2 | System | AAA Engine (SVC-04) | Accounting-Update packets arrive (usage: bytes in/out, duration) | CDR records created in TimescaleDB |
| 3 | System | Analytics (SVC-07) | CDR processing & rating engine runs | Usage rated with carrier costs, per-RAT-type cost applied |
| 4 | Analyst | SCR-011: Usage Analytics | View usage dashboard | Per-SIM/APN/operator/RAT-type usage charts populated |
| 5 | Analyst | SCR-012: Cost Analytics | View cost dashboard | Cost breakdown by operator, APN, RAT-type; optimization recommendations |
| 6 | System | Anomaly Detection (SVC-07) | One SIM shows 100× normal data usage | Anomaly flagged: "Data spike detected on SIM {ICCID}" |
| 7 | System | Notification (SVC-08) | — | Anomaly alert sent to configured channels |
| 8 | Analyst | SCR-013: Anomaly Dashboard | View anomalies | Data spike alert with SIM details, usage comparison chart |
| 9 | Analyst | SCR-100: Notifications | Check notifications | Anomaly notification with link to affected SIM |
| 10 | Analyst | SCR-021c: SIM Usage | Click through to SIM usage tab | Usage chart shows the spike clearly vs historical baseline |

### Verify (Post-Flow Checks)

- [ ] CDR records stored in TimescaleDB hypertable with correct timestamps
- [ ] Cost calculation applies correct per-operator, per-RAT-type rates
- [ ] Usage aggregations correct at SIM, APN, operator, and tenant levels
- [ ] Anomaly detection threshold correctly identifies the spike
- [ ] Dashboard charts update via WebSocket (real-time)
- [ ] Cost optimization suggestions based on cross-operator comparison

---

## UAT-011: RBAC Multi-Role Permission Enforcement

**Business Context**: Different roles must have strictly enforced access boundaries. An Analyst must not access policy management, and a SIM Manager must not modify tenant settings.
**Trigger**: Tenant Admin creates users with different roles
**Roles Involved**: Tenant Admin, Analyst, SIM Manager
**Business Rules**: BR-6 (Tenant Isolation)
**Stories**: STORY-004, STORY-005, STORY-003

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-110: Users & Roles | Create user "analyst@test.com" with role Analyst | User created |
| 2 | Tenant Admin | SCR-110: Users & Roles | Create user "simops@test.com" with role SIM Manager | User created |
| 3 | Analyst | SCR-001: Login | Login as analyst@test.com | Redirected to SCR-010 dashboard |
| 4 | Analyst | SCR-011: Usage Analytics | Navigate to usage analytics | Access granted — page loads with data |
| 5 | Analyst | SCR-060: Policy List | Navigate to /policies | Access denied — 403 Forbidden, redirected |
| 6 | Analyst | SCR-020: SIM List | Navigate to /sims | Access denied — 403 Forbidden |
| 7 | SIM Manager | SCR-001: Login | Login as simops@test.com | Redirected to SCR-010 dashboard |
| 8 | SIM Manager | SCR-020: SIM List | Navigate to SIM list | Access granted — SIM data visible |
| 9 | SIM Manager | SCR-110: Users & Roles | Navigate to /settings/users | Access denied — 403 Forbidden |
| 10 | SIM Manager | SCR-060: Policy List | Navigate to /policies | Access denied — 403 Forbidden |
| 11 | Tenant Admin | SCR-090: Audit Log | View audit log | Access denied attempts logged with user, role, endpoint |

### Verify (Post-Flow Checks)

- [ ] Analyst can access: SCR-010, SCR-011, SCR-012, SCR-013, SCR-100
- [ ] Analyst cannot access: SCR-020, SCR-030, SCR-040, SCR-060, SCR-110, SCR-090
- [ ] SIM Manager can access: SCR-010, SCR-020, SCR-021, SCR-030, SCR-050, SCR-070, SCR-080, SCR-100
- [ ] SIM Manager cannot access: SCR-060, SCR-110, SCR-090, SCR-121
- [ ] API returns 403 for unauthorized endpoints (not 404 — no information leakage)
- [ ] Navigation menu only shows permitted screens per role

---

## UAT-012: Audit Log Tamper Detection & Search

**Business Context**: Audit logs must be tamper-proof via hash chain, searchable, and show before/after diffs for every state-changing operation.
**Trigger**: Various state-changing operations across the system
**Roles Involved**: Tenant Admin
**Business Rules**: BR-7 (Audit & Compliance)
**Stories**: STORY-007, STORY-011, STORY-005

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-020: SIM List | Change SIM state (ACTIVE→SUSPENDED) | Audit entry created |
| 2 | Tenant Admin | SCR-110: Users & Roles | Update user role (Analyst→SIM Manager) | Audit entry created |
| 3 | Tenant Admin | SCR-030: APN List | Modify APN settings | Audit entry created |
| 4 | Tenant Admin | SCR-090: Audit Log | View audit log | All 3 entries visible with timestamps, actors, actions |
| 5 | Tenant Admin | SCR-090: Audit Log | Click on SIM state change entry | Before/after diff: `{state: "active"}` → `{state: "suspended"}` |
| 6 | Tenant Admin | SCR-090: Audit Log | Search by entity type "sim" | Filtered to SIM-related entries only |
| 7 | Tenant Admin | SCR-090: Audit Log | Search by date range | Entries filtered to selected period |
| 8 | System | Audit Service (SVC-10) | Hash chain verification | Each entry's hash = SHA256(prev_hash + entry_data) |

### Verify (Post-Flow Checks)

- [ ] Each audit entry has: who (user_id), when (timestamp), what (action), entity_type, entity_id
- [ ] Before/after JSON diffs are accurate and complete
- [ ] Hash chain: entry[n].prev_hash === entry[n-1].hash
- [ ] Audit log is append-only (no UPDATE/DELETE possible)
- [ ] Search by: entity_type, user_id, action, date range all functional
- [ ] Export to CSV works with all fields included

---

## UAT-013: Notification Multi-Channel Delivery

**Business Context**: Critical events must reach operators through multiple channels (in-app, email, webhook, Telegram) based on per-user configuration, with read/unread tracking.
**Trigger**: SIM state change triggers notification
**Roles Involved**: SIM Manager, Tenant Admin
**Business Rules**: BR-7 (Audit)
**Stories**: STORY-038, STORY-039, STORY-011

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-113: Notification Config | Configure: in-app=ON, webhook=ON (URL: https://webhook.test/argus), email=OFF | Config saved |
| 2 | SIM Manager | SCR-020: SIM List | Change SIM state to SUSPENDED | State change event emitted |
| 3 | System | Notification (SVC-08) | — | In-app notification created + webhook POST sent |
| 4 | SIM Manager | SCR-100: Notifications | Check notification bell | Badge shows unread count, notification visible |
| 5 | SIM Manager | SCR-100: Notifications | Click notification | Notification marked as read, badge count decremented |
| 6 | System | Webhook delivery | — | POST to webhook URL with event payload (SIM state change) |
| 7 | SIM Manager | SCR-010: Main Dashboard | Check dashboard | Alert feed shows SIM state change event |
| 8 | Tenant Admin | SCR-113: Notification Config | Verify email was NOT sent | No email delivery (disabled in config) |

### Verify (Post-Flow Checks)

- [ ] In-app notification stored with state UNREAD→READ on click
- [ ] Webhook POST sent with correct payload (event_type, entity, before/after)
- [ ] Email NOT sent (respecting config)
- [ ] Notification scoped to correct tenant
- [ ] Bell icon badge count accurate (real-time via WebSocket)
- [ ] Notification list supports cursor-based pagination

---

## UAT-014: API Key Lifecycle & Rate Limiting

**Business Context**: API keys must support scoped access and rate limiting to protect the platform from abuse while enabling programmatic integration.
**Trigger**: Tenant Admin creates an API key
**Roles Involved**: Tenant Admin, API User (external system)
**Business Rules**: BR-6 (Tenant Isolation)
**Stories**: STORY-008, STORY-004

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-111: API Keys | Create API key (name: "monitoring", scopes: [sim:read], rate_limit: 10/min) | Key generated, shown once (masked after) |
| 2 | API User | API | GET /api/sims with API key header | 200 OK — SIM list returned |
| 3 | API User | API | POST /api/sims with API key header | 403 Forbidden — scope "sim:write" not granted |
| 4 | API User | API | Send 10 GET /api/sims requests in 1 minute | All 10 succeed (within rate limit) |
| 5 | API User | API | Send 11th GET /api/sims in same minute | 429 Too Many Requests — rate limit exceeded |
| 6 | API User | API | Wait 1 minute, send another request | 200 OK — rate limit window reset |
| 7 | Tenant Admin | SCR-111: API Keys | Revoke the API key | Key marked as revoked |
| 8 | API User | API | GET /api/sims with revoked key | 401 Unauthorized — key revoked |
| 9 | Tenant Admin | SCR-090: Audit Log | View audit | api_key_created, rate_limit_exceeded, api_key_revoked events |

### Verify (Post-Flow Checks)

- [ ] API key hash stored (never plaintext)
- [ ] Scope enforcement at middleware level (not handler level)
- [ ] Rate limit counter resets correctly per window
- [ ] Revoked key immediately rejected (no cache delay)
- [ ] API key scoped to tenant (can't access other tenant's data)
- [ ] Audit log tracks all API key lifecycle events

---

## UAT-015: 2FA Enable → Login Flow

**Business Context**: Two-factor authentication adds a critical security layer. Users must be able to enable 2FA and the login flow must require TOTP verification.
**Trigger**: User enables 2FA on their account
**Roles Involved**: Tenant Admin
**Business Rules**: BR-7 (Audit)
**Stories**: STORY-003

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Tenant Admin | SCR-110: Users & Roles | Enable 2FA on own account | QR code displayed for TOTP app setup |
| 2 | Tenant Admin | SCR-110: Users & Roles | Scan QR code with authenticator app, enter verification code | 2FA enabled, backup codes provided |
| 3 | Tenant Admin | System | Logout | Session cleared |
| 4 | Tenant Admin | SCR-001: Login | Enter email + password | Credentials valid → redirected to SCR-002: 2FA Verification |
| 5 | Tenant Admin | SCR-002: 2FA Verification | Enter correct TOTP code | Login successful → redirected to SCR-010: Dashboard |
| 6 | Tenant Admin | System | Logout and login again | Redirected to SCR-002 again |
| 7 | Tenant Admin | SCR-002: 2FA Verification | Enter wrong TOTP code 3 times | Error: "Invalid code" + rate limiting applied |
| 8 | Tenant Admin | SCR-002: 2FA Verification | Enter correct code after cooldown | Login successful |
| 9 | Tenant Admin | SCR-090: Audit Log | View audit | 2fa_enabled, login_success, login_2fa_failed (×3), login_success events |

### Verify (Post-Flow Checks)

- [ ] 2FA secret stored encrypted in DB
- [ ] Login flow always requires 2FA after enablement
- [ ] Failed 2FA attempts rate-limited (not just counted)
- [ ] Backup codes work as alternative to TOTP
- [ ] Audit log captures all 2FA-related events
- [ ] Session JWT includes 2fa_verified claim

---

## UAT-016: RADIUS Authentication via Mock Operator

**Business Context**: The RADIUS AAA flow must work end-to-end through the mock operator adapter, from Access-Request to session establishment and accounting.
**Trigger**: Network Access Server (NAS) sends Access-Request for a SIM
**Roles Involved**: System (automatic)
**Business Rules**: BR-1 (SIM State), BR-4 (Policy)
**Stories**: STORY-015, STORY-018, STORY-011, STORY-017

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | RADIUS Server (:1812) | NAS sends Access-Request (User-Name=IMSI, NAS-IP, Called-Station-Id=APN) | Packet received by worker pool |
| 2 | System | SIM Cache | Lookup SIM by IMSI | SIM found in Redis cache (or DB fallback), state=ACTIVE verified |
| 3 | System | Operator Router (SVC-06) | Route to operator adapter based on SIM's operator_id | MockAdapter selected |
| 4 | System | MockAdapter | Authenticate(IMSI, APN) | Returns AuthResult{Accepted: true, SessionID: "mock-session-{IMSI}-1"} |
| 5 | System | Policy Engine (SVC-05) | Evaluate policy for SIM | QoS attributes determined (bandwidth, session timeout) |
| 6 | System | RADIUS Server | Build Access-Accept with policy attributes | Response sent to NAS with session-id, QoS AVPs |
| 7 | System | Session Manager | Create session record | Session stored in DB + Redis with SIM, operator, IP, RAT-type |
| 8 | System | RADIUS Server (:1813) | NAS sends Accounting-Start | Session start time recorded |
| 9 | System | RADIUS Server (:1813) | NAS sends Accounting-Update (bytes in/out, duration) | CDR record created in TimescaleDB |
| 10 | System | RADIUS Server (:1813) | NAS sends Accounting-Stop | Session ended, final CDR recorded, IP released (if dynamic) |

### Verify (Post-Flow Checks)

- [ ] Access-Accept contains correct RADIUS attributes (Session-Timeout, Filter-Id, etc.)
- [ ] Session record links SIM, operator, APN, IP, RAT-type correctly
- [ ] CDR records have accurate usage metrics (bytes, duration)
- [ ] SIM cache hit ratio tracked (Redis vs DB fallback)
- [ ] Metrics recorded: auth latency, success/reject counts
- [ ] SCR-050 shows active session during flow, removed after Accounting-Stop
- [ ] Audit log: session_created, session_ended events

---

## UAT-017: EAP-SIM/AKA Multi-Round Authentication

**Business Context**: SIM authentication via EAP requires multi-round challenge-response exchanges. The mock vector provider must generate correct 2G triplets (EAP-SIM) and 3G quintets (EAP-AKA) with deterministic reproducibility.
**Trigger**: NAS sends Access-Request with EAP-Identity
**Roles Involved**: System (automatic)
**Business Rules**: BR-1 (SIM State)
**Stories**: STORY-016, STORY-015, STORY-018

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | RADIUS Server | NAS sends Access-Request with EAP-Response/Identity (IMSI) | EAP state machine initialized, state: identity |
| 2 | System | EAP State Machine | Detect SIM type → 2G SIM → select EAP-SIM | Method negotiation |
| 3 | System | MockVectorProvider | FetchSIMTriplets(IMSI, 3) | 3 deterministic triplets generated (RAND×3, SRES×3, Kc×3) |
| 4 | System | RADIUS Server | Send Access-Challenge with EAP-Request/SIM-Start | Challenge containing AT_RAND×3, AT_MAC sent to NAS |
| 5 | System | RADIUS Server | NAS responds with EAP-Response/SIM-Challenge (AT_SRES) | SRES values validated against triplets |
| 6 | System | EAP State Machine | SRES match → derive MSK (64 bytes via HMAC-SHA1) | MS-MPPE-Send-Key (0:32) + MS-MPPE-Recv-Key (32:64) |
| 7 | System | RADIUS Server | Send Access-Accept with EAP-Success + MS-MPPE-Keys | Authentication complete |
| 8 | System | EAP State Machine | Repeat with 3G SIM → EAP-AKA | Method: EAP-AKA selected |
| 9 | System | MockVectorProvider | FetchAKAQuintet(IMSI) | Deterministic quintet (RAND, AUTN, XRES, CK, IK) |
| 10 | System | RADIUS Server | Access-Challenge with AT_RAND, AT_AUTN, AT_MAC | AKA challenge sent |
| 11 | System | RADIUS Server | NAS responds with AT_RES | XRES validated, MSK derived via HMAC-SHA256 |
| 12 | System | RADIUS Server | Access-Accept with EAP-Success | AKA authentication complete |

### Verify (Post-Flow Checks)

- [ ] EAP-SIM: 3 triplets used, SRES validation correct
- [ ] EAP-AKA: quintet used, XRES validation correct, AUTN verified
- [ ] MSK derivation correct (EAP-SIM: HMAC-SHA1, EAP-AKA: HMAC-SHA256)
- [ ] MS-MPPE-Keys correctly split and encrypted in RADIUS response
- [ ] Deterministic: same IMSI always produces identical vectors
- [ ] Vector cache in Redis (5-min TTL, batch pre-fetch)
- [ ] EAP pending challenges stored in Redis with 30s TTL
- [ ] State transitions logged: identity→challenge→success

---

## UAT-018: Diameter Gx/Gy Policy & Charging via Mock

**Business Context**: Diameter protocol must support policy control (Gx) and online charging (Gy) through the mock adapter, with correct session management and PCC rule installation.
**Trigger**: Diameter peer connects and sends CCR-Initial
**Roles Involved**: System (automatic)
**Business Rules**: BR-4 (Policy), BR-5 (Operator)
**Stories**: STORY-019, STORY-018, STORY-022

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | Diameter Server (:3868) | Peer connects via TCP | Connection accepted |
| 2 | System | Diameter Server | Peer sends CER (Capabilities-Exchange-Request) | CEA sent back, peer state: idle→open, applications negotiated |
| 3 | System | Diameter Server | DWR/DWA (Device Watchdog) exchange | Keepalive confirmed, peer health tracked |
| 4 | System | Diameter Server | Gx CCR-Initial (CC-Request-Type=1, IMSI, APN) | Session-Id generated, SIM looked up |
| 5 | System | Policy Engine (SVC-05) | Evaluate policy for SIM | PCC rules determined (QoS: 10Mbps DL, 5Mbps UL) |
| 6 | System | MockAdapter | Forward policy install | MockAdapter acknowledges |
| 7 | System | Diameter Server | CCA-Initial with Charging-Rule-Install AVPs (QoS-Information, Flow-Description, precedence) | PCC rules sent to peer |
| 8 | System | Diameter Server | Gy CCR-Initial (quota request) | Granted-Service-Unit: 1GB |
| 9 | System | Diameter Server | Gy CCR-Update (Used-Service-Unit: 500MB) | Remaining quota calculated, new grant if needed |
| 10 | System | Diameter Server | RAR (Re-Auth-Request) sent for mid-session policy change | Peer receives new PCC rules via RAA |
| 11 | System | Diameter Server | CCR-Termination (session end) | Session closed, final accounting recorded |
| 12 | System | Diameter Server | DPR/DPA (Disconnect-Peer-Request) | Peer gracefully disconnected, state: open→closing→closed |

### Verify (Post-Flow Checks)

- [ ] Diameter Session-Id format: `{DiameterIdentity};{high32};{low32};{optional}`
- [ ] Session-Id maps to acct_session_id in sessions table
- [ ] Peer state transitions: idle→open→closing→closed logged
- [ ] Gx PCC rules contain correct QoS-Information AVPs
- [ ] Gy quota tracking: granted - used = remaining
- [ ] RAR triggers mid-session policy update successfully
- [ ] CCR-Termination closes session and records final CDR
- [ ] Thread-safe peer tracking via sync.Map

---

## UAT-019: 5G SBA Authentication (AUSF/UDM) via Mock

**Business Context**: 5G Standalone networks use HTTP/2 based Service-Based Architecture. Argus must handle SUCI→SUPI resolution, 5G-AKA authentication, and key derivation through the mock adapter.
**Trigger**: 5G core sends authentication request to AUSF endpoint
**Roles Involved**: System (automatic)
**Business Rules**: BR-1 (SIM State)
**Stories**: STORY-020, STORY-016, STORY-018

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | SBA Server (:8443) | HTTP/2 POST `/nausf-auth/v1/ue-authentications` with SUCI | Request received |
| 2 | System | SBA Server | SUCI→SUPI resolution (suci-{MCC}{MNC}... → imsi-{IMSI}) | IMSI extracted from concealed identifier |
| 3 | System | SIM Cache | Lookup SIM by IMSI | SIM found, state=ACTIVE, operator determined |
| 4 | System | MockAdapter | FetchAuthVectors(IMSI, 1) → quintet | AV generated: RAND, AUTN, XRES*, KAUSF |
| 5 | System | SBA Server | Return 201 with auth challenge (RAND, AUTN, S-NSSAI) | 5G-AKA challenge sent to UE via core |
| 6 | System | SBA Server | PUT `/nausf-auth/v1/ue-authentications/{id}/5g-aka-confirmation` with RES* | Confirmation request received |
| 7 | System | SBA Server | Verify HXRES* (HMAC-SHA256 of XRES*) | Authentication verified |
| 8 | System | SBA Server | Derive KSEAF from KAUSF (HMAC-SHA256) | Security anchor key generated |
| 9 | System | SBA Server | Return 200 with KSEAF + auth result | 5G-AKA complete |
| 10 | System | Session Manager | Create 5G session with slice info (S-NSSAI: SST + SD) | Session stored with network slice context |

### Verify (Post-Flow Checks)

- [ ] SUCI→SUPI resolution correct (MCC+MNC extraction)
- [ ] Auth vectors: RAND (16B), AUTN (16B), XRES* (variable), KAUSF (32B)
- [ ] HXRES* verification uses HMAC-SHA256
- [ ] KSEAF derivation: HMAC-SHA256(KAUSF, serving_network_name)
- [ ] S-NSSAI (SST 1 byte + SD 3 bytes) stored in session
- [ ] HTTP/2 status codes correct (201 Created, 200 OK)
- [ ] UDM endpoint (`/nudm-ueau/v1/`) serves subscriber data
- [ ] Session links to correct operator and slice

---

## UAT-020: Circuit Breaker Lifecycle (Full State Machine)

**Business Context**: The circuit breaker must protect against cascade failures by detecting operator degradation, blocking requests during outage, and safely recovering through a half-open test phase.
**Trigger**: Mock adapter configured for initial failure then recovery
**Roles Involved**: System (automatic), Operator Manager
**Business Rules**: BR-5 (Operator Failover)
**Stories**: STORY-021, STORY-018, STORY-009

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Operator Manager | SCR-041: Operator Detail | Configure MockTurkcell: `{success_rate: 0, healthy_after: 10}` | Adapter will fail all requests until 10th call |
| 2 | System | AAA Engine | Auth request #1-#5 for Turkcell SIMs | All 5 rejected by MockAdapter |
| 3 | System | Circuit Breaker | Failure count = 5 (threshold reached) | State: CLOSED→OPEN |
| 4 | System | Operator Router | Operator marked DEGRADED | SCR-041 shows DEGRADED badge |
| 5 | System | Notification (SVC-08) | — | "Operator Turkcell circuit breaker OPEN" alert |
| 6 | System | AAA Engine | Auth request #6-#9 for Turkcell SIMs | Immediately rejected by circuit breaker (not forwarded to adapter) |
| 7 | System | Circuit Breaker | Recovery window elapses | State: OPEN→HALF-OPEN |
| 8 | System | AAA Engine | Auth request #10 (test request) | Forwarded to MockAdapter (callCount=10, healthy_after=10 → starts succeeding) |
| 9 | System | Circuit Breaker | Test request succeeds | State: HALF-OPEN→CLOSED |
| 10 | System | Operator Router | Operator restored to ACTIVE | SCR-041 shows ACTIVE badge |
| 11 | System | Notification (SVC-08) | — | "Operator Turkcell recovered" notification |
| 12 | Operator Manager | SCR-041: Operator Detail | View health timeline | Full state machine history: CLOSED→OPEN→HALF-OPEN→CLOSED with timestamps |

### Verify (Post-Flow Checks)

- [ ] Requests #6-#9 rejected without touching adapter (circuit breaker fast-fail)
- [ ] Recovery window is configurable per operator (CircuitBreakerRecoverySec)
- [ ] Threshold is configurable per operator (CircuitBreakerThreshold)
- [ ] HALF-OPEN allows exactly one test request
- [ ] If test request fails in HALF-OPEN → back to OPEN (not verified in this scenario)
- [ ] All state transitions logged in audit
- [ ] SLA violation event with exact downtime duration (OPEN start → CLOSED)
- [ ] Dashboard alert feed reflects state changes in real-time

---

## UAT-021: Mock Chaos Test (Partial Failure & Anomaly)

**Business Context**: Simulating partial operator failures validates that anomaly detection correctly identifies degraded service quality and alerts operators before full outage occurs.
**Trigger**: Mock adapter configured with 50% success rate
**Roles Involved**: System (automatic), Analyst, Operator Manager
**Business Rules**: BR-5 (Operator Failover), BR-7 (Audit)
**Stories**: STORY-021, STORY-036, STORY-033, STORY-018

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | Operator Manager | SCR-041: Operator Detail | Configure MockTurkcell: `{success_rate: 50, latency_ms: 200}` | Adapter will randomly fail ~50% of requests with 200ms latency |
| 2 | System | AAA Engine | Send 100 auth requests for Turkcell SIMs | ~50 accepted, ~50 rejected |
| 3 | System | Metrics (SVC-07) | Record auth success/failure rates | Success rate drops to ~50% (vs normal ~99%) |
| 4 | System | Anomaly Detection | Detect unusual reject rate pattern | Anomaly flagged: "High reject rate on operator Turkcell: 50% (baseline: <1%)" |
| 5 | System | Notification (SVC-08) | — | Anomaly alert sent to Operator Manager + Analyst |
| 6 | Analyst | SCR-013: Anomalies | View anomaly dashboard | Turkcell reject rate anomaly visible with timeline chart |
| 7 | Analyst | SCR-011: Usage Analytics | View usage dashboard | Auth success/failure ratio chart shows degradation |
| 8 | System | AAA Engine | Failed requests → retry via failover policy | Some retried to Vodafone (if failover=fallback-to-next) |
| 9 | Operator Manager | SCR-041: Operator Detail | View operator metrics | Latency P50=200ms, success_rate=50%, trend chart |
| 10 | System | Circuit Breaker | 50% success rate → below threshold? | Depends on config: may or may not trip breaker (configurable) |

### Verify (Post-Flow Checks)

- [ ] ~50 sessions created (accepted), ~50 auth failures logged
- [ ] Anomaly detection threshold correctly calibrated (baseline vs current)
- [ ] Metrics dashboard shows real-time auth rate degradation
- [ ] Latency injection reflected in P50/P95/P99 percentile charts
- [ ] Retry/failover policy correctly applied to failed requests
- [ ] No data corruption from concurrent partial failures (thread-safe MockAdapter)
- [ ] Alert contains: operator name, current rate, baseline rate, affected SIM count

---

## UAT-022: CoA/DM Session Control via Mock

**Business Context**: Change of Authorization (CoA) and Disconnect Message (DM) enable real-time session modification and termination. Policy changes and SIM suspensions must propagate to active sessions immediately.
**Trigger**: Policy change or SIM state change on a SIM with active session
**Roles Involved**: Policy Editor, SIM Manager
**Business Rules**: BR-4 (Policy Enforcement), BR-1 (SIM State)
**Stories**: STORY-017, STORY-025, STORY-011, STORY-018

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | System | AAA Engine | SIM authenticates via MockAdapter → session established | Active session in SCR-050 |
| 2 | SIM Manager | SCR-050: Live Sessions | View active sessions | Session visible with SIM ICCID, operator, IP, start time |
| 3 | Policy Editor | SCR-062: Policy Editor | Assign new policy version to SIM (bandwidth: 5Mbps→20Mbps) | Policy change event emitted |
| 4 | System | AAA Engine | MockAdapter.SendCoA(session_id, new_attributes) | CoA sent with updated QoS attributes |
| 5 | System | Session Manager | Update session record | Session's policy_version_id updated in DB |
| 6 | SIM Manager | SCR-050: Live Sessions | View session detail | Updated QoS attributes reflected |
| 7 | SIM Manager | SCR-021: SIM Detail | View SIM overview | New policy version shown |
| 8 | SIM Manager | SCR-021: SIM Detail | Click "Suspend" on SIM | SIM state → SUSPENDED |
| 9 | System | AAA Engine | MockAdapter.SendDM(session_id) | Disconnect Message sent, session terminated |
| 10 | SIM Manager | SCR-050: Live Sessions | View sessions | Session no longer in active list |
| 11 | SIM Manager | SCR-021: SIM Detail | View overview | "No active session" badge, state: SUSPENDED |
| 12 | SIM Manager | SCR-021b: SIM Sessions | View session history tab | Terminated session with end timestamp and reason: "DM - SIM suspended" |

### Verify (Post-Flow Checks)

- [ ] CoA updates session attributes without terminating the session
- [ ] DM terminates session immediately (session end time recorded)
- [ ] MockAdapter.SendCoA and SendDM both called correctly
- [ ] Session history shows both the CoA update and DM termination
- [ ] Policy version change propagated via CoA (not session restart)
- [ ] After DM: SIM cannot establish new session while SUSPENDED
- [ ] Audit log: policy_assigned, coa_sent, sim_suspended, dm_sent events
- [ ] SCR-050 updates in real-time via WebSocket

---

## UAT-023: OTA Command Delivery Simulation

**Business Context**: Over-The-Air SIM management sends APDU commands to SIM cards via SMS-PP or BIP channels with configurable security (encryption + MAC). The full delivery pipeline must work through the job system.
**Trigger**: Admin sends OTA command from SIM detail page
**Roles Involved**: SIM Manager
**Business Rules**: BR-7 (Audit)
**Stories**: STORY-029, STORY-031

### Steps

| # | Actor | Screen / System | Action | Expected Result |
|---|-------|----------------|--------|-----------------|
| 1 | SIM Manager | SCR-021: SIM Detail | Click "Send OTA Command" → select UPDATE_FILE | OTA command form shown |
| 2 | SIM Manager | SCR-021: SIM Detail | Configure: data=file_content, security=kic_kid, channel=sms_pp | Command parameters set |
| 3 | System | OTA Service | BuildAPDU(UPDATE_FILE, data) | APDU command constructed |
| 4 | System | OTA Security | Apply KIC encryption (AES-128-CBC) + KID MAC (HMAC-SHA256, truncated 8B) | Secured APDU ready |
| 5 | System | OTA Delivery | Build SMS-PP envelope (GSM 03.48): SPI + KIC/KID indicators + TAR + CNTR + MAC + encrypted APDU | Envelope ≤140 bytes |
| 6 | System | Job Runner (SVC-09) | OTA delivery job created | Job visible in SCR-080, status: Queued |
| 7 | System | Job Runner | Job executes: send via SMS gateway | Status: Queued→Sent |
| 8 | System | OTA Delivery | Delivery acknowledgment received | Status: Sent→Delivered |
| 9 | System | OTA Delivery | Execution confirmation from SIM | Status: Delivered→Executed→Confirmed |
| 10 | SIM Manager | SCR-080: Job List | View OTA job | Final status: Confirmed with execution result |
| 11 | SIM Manager | SCR-021: SIM Detail | View OTA history | Command entry with: type, security mode, channel, status, timestamps |

### Verify (Post-Flow Checks)

- [ ] APDU correctly built for UPDATE_FILE command type
- [ ] KIC encryption: AES-128-CBC with correct key
- [ ] KID MAC: HMAC-SHA256 truncated to 8 bytes
- [ ] SMS-PP envelope follows GSM 03.48 format (SPI, TAR, CNTR, MAC, data)
- [ ] Envelope size ≤ 140 bytes (SMS limit)
- [ ] OTA rate limiting enforced (per-SIM command throttle)
- [ ] Status lifecycle: Queued→Sent→Delivered→Executed→Confirmed
- [ ] Failed delivery creates retry job
- [ ] Audit log: ota_command_sent, ota_command_delivered, ota_command_confirmed events
- [ ] BIP channel alternative works with correct framing (channel_id + transport + port + data)

---

## Coverage Matrix

### Story Coverage

| Story | UAT Scenarios | Covered By |
|-------|--------------|------------|
| STORY-001 | 1 | UAT-001 |
| STORY-002 | 1 | UAT-001 |
| STORY-003 | 3 | UAT-001, UAT-011, UAT-015 |
| STORY-004 | 2 | UAT-011, UAT-014 |
| STORY-005 | 3 | UAT-001, UAT-011, UAT-012 |
| STORY-006 | 1 | UAT-001 |
| STORY-007 | 3 | UAT-002, UAT-003, UAT-012 |
| STORY-008 | 1 | UAT-014 |
| STORY-009 | 3 | UAT-001, UAT-005, UAT-020 |
| STORY-010 | 4 | UAT-001, UAT-008, UAT-009, UAT-007 |
| STORY-011 | 8 | UAT-001, UAT-002, UAT-003, UAT-007, UAT-008, UAT-009, UAT-013, UAT-016, UAT-022 |
| STORY-012 | 1 | UAT-006 |
| STORY-013 | 2 | UAT-001, UAT-002 |
| STORY-014 | 1 | UAT-001 |
| STORY-015 | 5 | UAT-003, UAT-007, UAT-016, UAT-017, UAT-019 |
| STORY-016 | 2 | UAT-017, UAT-019 |
| STORY-017 | 3 | UAT-003, UAT-004, UAT-022 |
| STORY-018 | 6 | UAT-005, UAT-006, UAT-016, UAT-017, UAT-018, UAT-020, UAT-021, UAT-022 |
| STORY-019 | 1 | UAT-018 |
| STORY-020 | 1 | UAT-019 |
| STORY-021 | 3 | UAT-005, UAT-020, UAT-021 |
| STORY-022 | 3 | UAT-001, UAT-004, UAT-018 |
| STORY-023 | 1 | UAT-004 |
| STORY-024 | 1 | UAT-004 |
| STORY-025 | 2 | UAT-004, UAT-022 |
| STORY-026 | 1 | UAT-006 |
| STORY-027 | 1 | UAT-016 |
| STORY-028 | 1 | UAT-006 |
| STORY-029 | 1 | UAT-023 |
| STORY-030 | 1 | UAT-006 |
| STORY-031 | 3 | UAT-002, UAT-006, UAT-023 |
| STORY-032 | 1 | UAT-010 |
| STORY-033 | 2 | UAT-005, UAT-021 |
| STORY-034 | 1 | UAT-010 |
| STORY-035 | 1 | UAT-010 |
| STORY-036 | 2 | UAT-010, UAT-021 |
| STORY-037 | 1 | UAT-007 |
| STORY-038 | 4 | UAT-001, UAT-002, UAT-005, UAT-009, UAT-013 |
| STORY-039 | 4 | UAT-001, UAT-002, UAT-009, UAT-013 |
| STORY-040 | 1 | UAT-010 |
| STORY-041 | 1 | UAT-012 |
| STORY-042 | 1 | UAT-001 |
| STORY-043 | 1 | UAT-010 |
| STORY-044 | 1 | UAT-003 |
| STORY-045 | 1 | UAT-001 |
| STORY-046 | 1 | UAT-011 |
| STORY-047 | 1 | UAT-007 |
| STORY-048 | 1 | UAT-015 |
| STORY-049 | 1 | UAT-013 |
| STORY-050 | 1 | UAT-011 |
| STORY-051 | 1 | UAT-015 |
| STORY-052 | 1 | UAT-014 |
| STORY-053 | 1 | UAT-001 |
| STORY-054 | 1 | UAT-014 |
| STORY-055 | 1 | UAT-001 |

### Business Rule Coverage

| Rule | Description | UAT Scenarios |
|------|-------------|--------------|
| BR-1 | SIM State Transitions | UAT-003, UAT-006, UAT-016, UAT-019, UAT-022 |
| BR-2 | APN Deletion Rules | UAT-008 |
| BR-3 | IP Address Management | UAT-002, UAT-003, UAT-009 |
| BR-4 | Policy Enforcement | UAT-003, UAT-004, UAT-016, UAT-018, UAT-022 |
| BR-5 | Operator Failover | UAT-005, UAT-020, UAT-021 |
| BR-6 | Tenant Isolation | UAT-001, UAT-011, UAT-014 |
| BR-7 | Audit & Compliance | UAT-002, UAT-003, UAT-010, UAT-012, UAT-013, UAT-014, UAT-015, UAT-021, UAT-023 |

### Screen Coverage

| Screen | UAT Scenarios |
|--------|--------------|
| SCR-001: Login | UAT-001, UAT-011, UAT-015 |
| SCR-002: 2FA Verification | UAT-015 |
| SCR-003: Onboarding Wizard | UAT-001 |
| SCR-010: Main Dashboard | UAT-001, UAT-002, UAT-005, UAT-009, UAT-013 |
| SCR-011: Usage Analytics | UAT-010, UAT-021 |
| SCR-012: Cost Analytics | UAT-010 |
| SCR-013: Anomalies | UAT-005, UAT-010, UAT-021 |
| SCR-020: SIM List | UAT-001, UAT-002, UAT-003, UAT-004, UAT-006, UAT-007, UAT-008, UAT-009, UAT-011, UAT-013 |
| SCR-021: SIM Detail | UAT-003, UAT-006, UAT-007, UAT-012, UAT-022, UAT-023 |
| SCR-021b: SIM Sessions | UAT-022 |
| SCR-021c: SIM Usage | UAT-010 |
| SCR-021d: SIM Diagnostics | UAT-007 |
| SCR-021e: SIM History | UAT-003 |
| SCR-030: APN List | UAT-001, UAT-007, UAT-008 |
| SCR-032: APN Detail | UAT-008 |
| SCR-040: Operator List | UAT-001, UAT-005 |
| SCR-041: Operator Detail | UAT-005, UAT-020, UAT-021 |
| SCR-050: Live Sessions | UAT-003, UAT-004, UAT-010, UAT-016, UAT-022 |
| SCR-060: Policy List | UAT-001, UAT-004, UAT-011 |
| SCR-062: Policy Editor | UAT-004, UAT-022 |
| SCR-070: eSIM Profiles | UAT-006 |
| SCR-080: Job List | UAT-002, UAT-006, UAT-023 |
| SCR-090: Audit Log | UAT-002, UAT-003, UAT-006, UAT-008, UAT-011, UAT-012, UAT-014 |
| SCR-100: Notifications | UAT-002, UAT-005, UAT-009, UAT-010, UAT-013 |
| SCR-110: Users & Roles | UAT-011, UAT-015 |
| SCR-111: API Keys | UAT-014 |
| SCR-112: IP Pools | UAT-009 |
| SCR-113: Notification Config | UAT-013 |
| SCR-120: System Health | UAT-005 |
| SCR-121: Tenant Management | UAT-001 |

### Mock Service Coverage

| Mock Service | UAT Scenarios |
|-------------|--------------|
| MockAdapter (Auth/Acct/CoA/DM) | UAT-016, UAT-017, UAT-018, UAT-020, UAT-021, UAT-022 |
| MockAdapter (Vector Generation) | UAT-016, UAT-017, UAT-019 |
| MockAdapter (Health Check) | UAT-005, UAT-020 |
| MockAdapter (Chaos Config) | UAT-021 |
| MockSMDPAdapter (eSIM) | UAT-006 |
| MockVectorProvider (EAP) | UAT-017 |
| Circuit Breaker | UAT-005, UAT-020, UAT-021 |
| OTA Delivery (SMS-PP/BIP) | UAT-023 |
| LoadGenerator (Bench) | UAT-021 |
