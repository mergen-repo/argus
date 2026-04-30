# UAT Batch 2 Remediation Plan

> **Trigger:** `docs/reports/uat-acceptance-2026-04-30.md` — REJECTED (6 CRITICAL + 9 HIGH + 5 MEDIUM)
> **Created:** 2026-04-30
> **Scope:** 11 BUG findings (F-1..F-11) from full UAT-001..023 run
> **Stop condition:** All CRITICAL+HIGH FIX stories DONE → re-run UAT acceptance → 0 CRITICAL/HIGH remaining
> **STALE_SCENARIO** (7 items) routed separately to UAT.md edit (no code change)
> **DATA_GAP** (5 items) documented, no fix required

---

## Sequencing Strategy

CRITICAL stories run FIRST and INDEPENDENTLY (no inter-deps among the 4). HIGH stories follow. F-1 (onboarding) and F-3 (audit chain) are batch1 RECURRENCES — each plan must include a regression-prevention section explaining why prior fix did not stick.

| Tier | FIX | Severity | UAT | Depends | Effort | Story |
|---|---|---|---|---|---|---|
| **T1: CRITICAL — Production Blockers** | | | | | | |
| 1 | FIX-301 | CRITICAL | UAT-002 + all tenant_admin SIM ops | — | M | F-2 Startup race: prepared-statement OID cache vs migration order |
| 2 | FIX-302 | CRITICAL | UAT-001 V10, UAT-012 step 8 | — | M | F-3 Audit hash chain broken at entry 1 — chain genuinely broken (verify code correct; insert/seed wrong prev_hash for entry 1) |
| 3 | FIX-303 | CRITICAL | UAT-001 step 3.5 | — | **S** | F-1 Onboarding wizard EXISTS at `/setup` — needs `/onboarding` alias + first-login redirect (not full re-implementation) |
| 4 | FIX-304 | CRITICAL | UAT-019 | — | S | F-4 5G SBA :8443 listener not bound inside container |
| **T2: HIGH — Functional Gaps** | | | | | | |
| 5 | FIX-305 | HIGH | UAT-003 step 4a, UAT-022 | — | M | F-5 SIM suspend handler does not auto-fire DM to active sessions |
| 6 | FIX-306 | HIGH | UAT-010 step 8, UAT-021 | — | S | F-6 `GET /api/v1/anomalies` listing route 404 |
| 7 | FIX-307 | HIGH | UAT-001 step 2, UAT-013 | — | M | F-7 Email pipeline silent — Mailhog 0 messages throughout UAT |
| **T3: MEDIUM — Data/DTO Drift** | | | | | | |
| 8 | FIX-308 | MEDIUM | UAT-005, UAT-020 | — | XS | F-8 `operators.circuit_state` column always NULL despite log transitions |
| 9 | FIX-309 | MEDIUM | UAT-013 step 1 | — | XS | F-9 `notification_preferences` empty for all users — defaults not seeded |
| 10 | FIX-310 | MEDIUM | UAT-023 step 6 | — | S | F-10 OTA POST 201 returns command id but row not in `esim_ota_commands` |
| 11 | FIX-311 | MEDIUM | UAT-003 | FIX-242 | XS | F-11 `ip_address` NULL in `/sims/{id}` DTO after activate/resume |

**Total effort:** 4 M + 3 S + 1 L + 3 XS ≈ 1.5–2 weeks autopilot

---

## Tier 1 — CRITICAL (run in parallel possible — independent)

### FIX-301: Startup Race — Prepared-Statement OID Cache vs Migration Order
- **Symptom:** Fresh `make up` → app starts → all `tenant_admin` SIM ops + `POST /sims/bulk/import` fail with `could not open relation with OID 105648`. App restart clears it.
- **Root cause hypothesis:** pgx (or sqlx) prepared-statement plan cache references OIDs from before final migration ran, OR seed/migration runs concurrently with first DB queries during app boot.
- **Investigation:** `grep -r "Prepare\|prepared_statement_cache" internal/store/`; check `cmd/argus/main.go` boot order — does seeder lock the connection pool before HTTP listener opens?
- **Fix candidates:** (a) issue `DISCARD ALL` on every connection acquired post-migration, (b) delay HTTP listener until migrations + seed complete, (c) drop pgx prepared-statement caching for tenant-scoped queries.
- **AC:** Fresh `make down && make up` → no restart needed; `POST /sims/bulk/import` works on first try.
- **Regression test:** Boot 10x in CI, run import on each.

### FIX-302: Audit Hash Chain Broken at Entry 1 — RECURRENCE (batch1 F-10)
- **Triage 2026-04-30:** Verify endpoint returns `total_rows:1` because verifier short-circuits when entry 1's `prev_hash != GenesisHash` (correct behavior — `internal/audit/service.go:138-145`). The chain is GENUINELY broken at row 1. FIX-104 (commit 2d7f917) shipped audit chain transactional write but did not enforce that the first audit row uses GenesisHash as prev_hash — likely seed data or non-transactional inserts (e.g., during boot) skip this.
- **Investigation:** `SELECT id, prev_hash, action FROM audit_logs ORDER BY id LIMIT 1;` → if prev_hash != `0000...0` (or whatever GenesisHash is), seed/boot path wrote wrong prev_hash. Trace inserter.
- **Regression-prevention:** add E2E test that fresh-boots app + inserts 100 audit rows → asserts `verify` returns `verified:true, total_rows:100`. Phase Gate must include this test path.
- **AC:** verify returns `verified:true` and `total_rows` matches `SELECT count(*) FROM audit_logs`.

### FIX-303: Onboarding Route Alias + First-Login Redirect — RECURRENCE (batch1 F-9)
- **Triage 2026-04-30:** Wizard EXISTS — `web/src/pages/auth/onboarding.tsx` registered at `/setup` (router.tsx:113). `internal/store/onboarding_session_store.go` exists with full session backend. UAT failure is routing-only:
  1. UAT.md and prior expectations use `/onboarding`, current code uses `/setup` — needs alias OR rename
  2. After tenant_admin first login, no auto-redirect to wizard — lands on Dashboard
- **Fix scope:** (a) add `/onboarding` route alias (or rename `/setup` → `/onboarding` if UAT.md is canonical), (b) add first-login redirect logic in login response handler / FE auth context — if `tenant.onboarded_at IS NULL` then redirect to wizard.
- **Effort dropped from L to S** — page implementation already done.
- **Regression-prevention:** UAT-001 smoke test that asserts `/onboarding` 200 + tenant_admin first login redirect → wizard. Phase Gate UI test path.
- **AC:** New tenant_admin first login → auto-redirect to wizard → completes → tenant marked onboarded → second login lands on dashboard.

### FIX-304: 5G SBA Listener Not Bound on :8443
- **Symptom:** Inside `argus-app` container: `wget https://localhost:8443/-/health` → connection refused on both `::1` and `127.0.0.1`. Other listeners (8080, 8081, 3868, 1812 UDP) work.
- **Investigation:** Search `cmd/argus/main.go` and `internal/aaa/sba/` for SBA startup. Check env flag `SBA_ENABLED` default; check TLS cert generation path; check if listener errored silently during startup (look at app logs).
- **AC:** SBA `:8443/-/health` responds 200; UAT-019 5G AUSF/UDM flow runs end-to-end.

---

## Tier 2 — HIGH

### FIX-305: SIM Suspend Does Not Auto-Fire DM
- **Symptom:** Suspend SIM with active session → session stays `active`, no `session.dm_sent` audit. Existing `session.dm_sent` events in DB are from synthetic Mock NAS load (separate code path).
- **Investigation:** `internal/api/sim.go` Suspend handler — does it enqueue a CoA/DM job? Compare to expected flow in `docs/architecture/PROTOCOLS.md`.
- **AC:** Suspend SIM with active session → `session.dm_sent` audit within 5s, `session_state` transitions to `terminated`.

### FIX-306: `/api/v1/anomalies` Listing Route Missing
- **Symptom:** 404. DB has 10 anomaly rows but no API to list them. Cascades to UAT-010 step 8 + entire UAT-021.
- **Investigation:** `internal/api/anomaly.go` (or wherever) — handler may exist but route not registered, or table exists but listing endpoint never built.
- **AC:** `GET /api/v1/anomalies` returns paginated list with cursor support; supports filters (severity, type, time range).

### FIX-307: Email Pipeline Silent
- **Symptom:** Mailhog `/api/v2/messages` total=0 throughout entire UAT run. No tenant invite, no bulk-import notification, no anomaly email fires.
- **Investigation:** Check SMTP env config in `deploy/docker-compose.yml` (`SMTP_HOST=mailhog:1025`?); inspect `internal/notification/email/` for circuit-breaker/skip flags; verify worker/queue is consuming notification jobs.
- **AC:** Tenant create → invite email visible in Mailhog; bulk import → completion email visible; anomaly trigger → alert email visible.

---

## Tier 3 — MEDIUM

### FIX-308: `operators.circuit_state` Always NULL
- **Symptom:** Column never populated despite `operator_health_logs` recording transitions.
- **Fix:** wire CB transition handler to UPDATE `operators.circuit_state` alongside log insert.

### FIX-309: `notification_preferences` Defaults Not Seeded
- **Symptom:** API returns `[]` for all users.
- **Fix:** Add to seed 004_notification_templates.sql (or new file) — default tier rows for each user role.

### FIX-310: OTA Command Not Persisted
- **Symptom:** POST `/sims/{id}/ota` → 201 with command id, but row absent in `esim_ota_commands`.
- **Investigation:** Handler may queue async without committing the row, or job worker creates the row only on execution. Either fix queueing to insert immediately (queued state) or document async pattern.

### FIX-311: `ip_address` NULL in SIM DTO After Activate/Resume
- **Symptom:** `/sims/{id}` returns `ip_address:null` for SIMs with allocated IP. Likely related to FIX-242 (Session Detail DTO populate).
- **Fix:** Extend FIX-242 join/populate logic to SIM detail DTO.

---

## STALE_SCENARIO Edits — `docs/UAT.md` (separate work, no code)

| ID | UAT# | Edit |
|---|---|---|
| D-uat-002-cols | UAT-002 | CSV columns: `iccid, imsi, msisdn, operator_code, apn_name, ip_address` (case-sensitive lower) |
| D-uat-004 | UAT-004 | Add `scope` field (cohort/segment) to step 2 |
| D-uat-006 | UAT-006 | Re-document eSIM endpoints against current FIX-235 pipeline (drop `/sim-segments`, `/sims/{id}/switch-operator`) |
| D-uat-007 | UAT-007 | Endpoint: `/diagnose` (not `/diagnostics`) |
| D-uat-011 | UAT-011 | Reconcile SIM Manager `/policies` access — pick one as canonical (UAT.md vs RBAC config) |
| D-uat-016 | UAT-016 | Use `sessions WHERE protocol_type='radius'` (drop `radius_sessions` table reference) |
| D-uat-022 | UAT-022 | Verify SCR-021b session history tab existence; remove if not implemented |

Route via `/amil change` (UAT update), single batch.

---

## Re-Acceptance Gate

After FIX-301..FIX-307 DONE (CRITICAL + HIGH):
1. Re-run `/amil acceptance` (or `/amil` UAT mode) on UAT-001..023
2. Required: 0 CRITICAL, 0 HIGH from this run's findings
3. Re-verify previous batch1 fixes did not regress again

After FIX-308..FIX-311 DONE (MEDIUM):
1. Re-run UAT for affected scenarios only (UAT-005, 013, 020, 023, 003)
2. Required: 0 MEDIUM remaining

---

## Recurrence Pattern Investigation

F-1 (FIX-303) and F-3 (FIX-302) are RECURRENCES from batch1 (2026-04-18). Pattern needs root-cause analysis:

- Did batch1 fixes ship to `main`? `git log --all --grep 'F-9\|F-10' --since 2026-04-18`
- If shipped, what regressed them? `git log -- internal/audit/verify.go internal/api/onboarding*` since fix dates
- Phase Gate test gap: neither bug was caught by Phase Gate runs in Wave 9 P1 + Wave 10 P2.

**Action:** PAT-027 candidate — "RECURRENCE: critical UAT bugs not caught by Phase Gate." Open a meta-finding to extend Phase Gate UI/API test suite to include UAT-001 + UAT-012 smoke checks before any phase closes.
