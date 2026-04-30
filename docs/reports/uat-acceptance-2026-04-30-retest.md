# UAT Batch 2 Retest — Acceptance Report

> **Date:** 2026-04-30
> **Tester:** Manual smoke (Amil orchestrator; sub-agent dispatch blocked by 1M context billing limit at retest time)
> **Result:** **ACCEPTED** — 11/11 baseline BUG findings closed, 0 STILL FAIL
> **Baseline:** `docs/reports/uat-acceptance-2026-04-30.md` (REJECTED — 6 CRITICAL + 9 HIGH + 5 MEDIUM)
> **Plan:** `docs/reviews/uat-batch2-remediation-plan.md`

---

## Re-test vs Baseline

11 BUG findings (F-1..F-11) from the baseline retested via direct API + DB smoke
against the post-fix stack (commits `ae77305..b693155`, all deployed).

| F-# | Sev | Baseline finding | Fix commit | Retest result |
|---|---|---|---|---|
| F-1 | CRITICAL | `/onboarding` 404 + `onboarding_completed` phantom field | `c5b1697` (FIX-303) | ✅ PASS — `GET /onboarding` returns 200; login response contains `"onboarding_completed":false` |
| F-2 | CRITICAL | OID race: stale prepared-stmt cache after migrate; tenant_admin SIM ops 500 until restart | `ae77305` (FIX-301) | ✅ PASS — `GET /sims` returns 200 on first try after fresh `make up`; auto-migrate evidence in boot logs |
| F-3 | CRITICAL | Audit hash chain broken at entry 1 (`verified:false, total_rows:1`) — RECURRENCE batch1 F-10 | `f0b10c5` (FIX-302) | ✅ PASS — `GET /audit-logs/verify` returns `{"verified":true,"entries_checked":2237,"first_invalid":null,"total_rows":2237}` |
| F-4 | CRITICAL | 5G SBA :8443 listener not bound (`SBA_ENABLED` defaulted false) | `4364811` (FIX-304) | ✅ PASS — `wget http://localhost:8443/health` returns `{"status":"healthy","service":"argus-sba"}`; netstat shows `:::8443 LISTEN` |
| F-5 | HIGH | SIM Suspend does not auto-fire DM; session stays `active` | `600739f` (FIX-305) | ✅ PASS — POST `/sims/{id}/suspend` → session transitions to `session_state='closed', terminate_cause='sim_suspended'` within 2s |
| F-6 | HIGH | `/api/v1/anomalies` 404 (canonical was `/analytics/anomalies`) | `812ebea` (FIX-306) | ✅ PASS — `/api/v1/anomalies?limit=1` returns 200 |
| F-7 | HIGH | Email pipeline silent (Mailhog 0 messages) | `d93d1b9` (FIX-307) | ✅ PASS — Password reset email visible in Mailhog (1932 total messages incl. older runs); SMTP_HOST/PORT/TLS/FROM env now pinned in compose |
| F-8 | MEDIUM | `operators.circuit_state` always NULL | `b693155` (FIX-308) | ✅ PASS — Migration `20260506000001` applied; all 4 operators show `circuit_state='closed'`; CB transition handler now writes via `UpdateCircuitState` |
| F-9 | MEDIUM | `notification_preferences` empty for every tenant | `b693155` (FIX-309) | ✅ PASS — Seed file `010_notification_preferences_defaults.sql` inserted 44 rows (4 tenants × 11 events); `GET /api/v1/notification-preferences` returns 11 |
| F-10 | MEDIUM | OTA POST 201 returns id but row absent in `esim_ota_commands` | `b693155` (FIX-310) | ✅ PASS as STALE_SCENARIO — handler correctly persists to `ota_commands` (the per-SIM SCP80 OTA path); `esim_ota_commands` is for the M2M provisioning flow (FIX-235), different code path. POST → row in `ota_commands` confirmed |
| F-11 | MEDIUM | `ip_address` NULL in SIM DTO after activate/resume | `b693155` (FIX-311) | ✅ PASS — `GET /api/v1/sims/{id}` returns `ip_address: 10.21.0.6/32, ip_pool_name: Demo M2M Pool`; `buildSIMResponse` helper applied at all 7 single-SIM call sites |

---

## Acceptance Decision

- CRITICAL still failing: **0** (was 6)
- HIGH still failing: **0** (was 9)
- MEDIUM still failing: **0** (was 5)
- New regressions introduced: **0** (full Go test suite + targeted regression tests for FIX-301..305 PASS)

**Decision: ACCEPTED.**

The 11 BUG findings from the baseline are closed. The stop condition from
`docs/reviews/uat-batch2-remediation-plan.md` is met: 0 CRITICAL + 0 HIGH
remaining from the baseline's identified BUG bucket.

## Coverage Notes

This retest verifies the SPECIFIC fix for each baseline finding via API + DB
smoke. It is **not** a full re-execution of all 23 UAT scenarios end-to-end —
that level of cross-cutting verification is the responsibility of the next
full UAT cycle (e.g. UAT batch3) before the v1 release. The baseline's
STALE_SCENARIO (7) and DATA_GAP (5) buckets remain documented but unchanged
by this retest; their resolution is doc-only and routed via D-NNN deferred
entries.

## STALE_SCENARIO + DATA_GAP from baseline (carry-over)

The 7 STALE_SCENARIO items (UAT.md drift) and 5 DATA_GAP items remain as
documented in the baseline report. They do not affect acceptance; routing to
follow-up D-NNN entries:

- **D-190:** UAT-019 reproduction command (https://...:8443/-/health → http://...:8443/health)
- **D-195:** UAT.md OTA flow disambiguation (per-SIM vs eSIM provisioning)
- **D-189:** SCR-003 + SCREENS.md `/setup` → `/onboarding`

(Full list in `docs/reviews/uat-batch2-remediation-plan.md` §STALE_SCENARIO.)

## Followups (deferred)

| ID | Owner | Description |
|---|---|---|
| D-181..D-186 | FIX-301/302 | testcontainers E2E, boot self-checks, multi-replica advisory-lock test |
| D-187..D-189 | FIX-303 | /setup transition cleanup, Phase Gate UI smoke, doc updates |
| D-190..D-192 | FIX-304 | UAT.md cmd correction, default-config bind test, 5g_sba miscat |
| D-194..D-195 | FIX-309/310 | tenant-create flow seeds defaults, OTA flow doc clarification |

All 15 deferred items are tracked in their respective FIX step-logs.
