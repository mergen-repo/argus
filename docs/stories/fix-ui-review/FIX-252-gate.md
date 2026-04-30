# Gate Report: FIX-252 — sim-activate-500-ip-pool (PAT-023 schema drift)

**RETROACTIVE GATE filed 2026-04-30 (Gate originally bypassed via user option-1 on 2026-04-26).**

> Original closure: 2026-04-26 (commit `b5e3ac0`)
> Retroactive Gate: 2026-04-30 — backfill missing artifact per protocol normalization.

## Bypass Rationale (original 2026-04-26)

Per `FIX-252-step-log.txt` line 4:
- `STEP_2.0 DRIFT-AUDIT: result=ESCALATED` — schema drift discovered (DB at `version=20260430000001 dirty=f` BUT `ip_addresses.last_seen_at` column missing, plus `password_reset_tokens` table missing).
- `STEP_2.1 USER-DECISION: option=1` — user chose db-reset (DROP SCHEMA public CASCADE → migrate-up → seed) and explicitly declined defensive code in this story scope.
- Defensive ACs (suspend atomic IP release, activate empty-pool guard, audit-on-failure, Resume re-allocation) spun off to **FIX-253** instead of being deferred as Tech Debt.
- Step-log `STEP_6 POSTPROC` records: `gate.md-N/A-escalation-bypassed-Gate-per-user-option-1+review.md-N/A-lite-review-inline`.

This retroactive Gate validates the SHIPPED state AS-IS — no new code changes, only verification that the recovery held and the spinoff covers the original ACs.

## Summary

- Requirements Tracing: Activate endpoint 1/1, schema migrations 2/2 reapplied (last_seen_at + password_reset_tokens)
- Gap Analysis: 5/5 ACs satisfied (4 by FIX-253 spinoff, 1 by db-reset recovery)
- Compliance: COMPLIANT — zero-code closure, no architectural deviation
- Tests: 560 PASS / 0 FAIL (`go test ./internal/api/sim/... ./internal/store/...`); 14/14 PASS for Activate|Suspend pattern
- Build: PASS (`go build ./...`)
- Vet: clean (`go vet ./...`)
- Schema drift fix: HELD (verified 2026-04-30)
- PAT-023 systemic risk: NO RECURRENCE (spot-checked sims/sessions/alerts/ip_addresses/password_reset_tokens — all schemas match migrations)
- Live verify: HTTP 200/200 round-trip suspend → activate
- Migrations idempotent: `make db-migrate` reports "no change — already at latest version"
- Overall: **PASS (RETROACTIVE)**

## Team Composition

- Single retroactive Gate Lead reviewer (no live scout dispatch — backfill mode)
- Original execution did NOT dispatch scouts (zero-code closure with user-approved bypass)
- All Pass 1-5 checks performed inline against current main (HEAD past commit `4663b03`)

## AC-by-AC Verification

| AC | Description | Closure Path | Verified |
|----|-------------|--------------|----------|
| AC-1 | Suspend → activate round-trip succeeds | Schema-drift fix (DB reset) + FIX-253 store hardening | LIVE 200/200 |
| AC-2 | Pool-cannot-allocate → structured 4xx | FIX-253 Handler.Activate empty-pool guard → 422 POOL_EXHAUSTED | FIX-253 tests + commit 95856fb |
| AC-3 | Stack + correlation ID + audit on success AND failure | FIX-253 sim.activate.failed audit on 7+ branches | FIX-253 tests |
| AC-4 | Unit test: activate-after-suspend round-trip | FIX-253 `internal/store/sim_suspend_test.go` (8 cases) | `go test` 14/14 PASS |
| AC-5 | SIM `fffa41ad-…` reactivatable | `make db-seed` recovery — SIM ID-space reset; symptom path validated on clean SIM `b1b5af33-…` | LIVE 200/200 |

## Fixes Applied

None — this is a retroactive verification Gate. The fix shipped was zero-code:

| # | Action | Evidence |
|---|--------|----------|
| 1 | DROP SCHEMA public CASCADE + migrate-up + seed | step-log STEP_2.2; backup `backups/pre-fix252-reset-20260426-204557.sql` (7.1MB) |
| 2 | Defensive code spun off to FIX-253 | commit `95856fb` — 11 regression tests, all PASS |
| 3 | Decisions logged: DEV-386 (closure rationale), DEV-387 (drift RCA), DEV-388 (spinoff rationale) | `docs/brainstorming/decisions.md` lines 621-623 |
| 4 | PAT-023 added: schema_migrations can lie | `docs/brainstorming/bug-patterns.md` line 32 |
| 5 | USERTEST FIX-252 section: 4 Turkish scenarios | `docs/USERTEST.md` line 4694 |

## Verification (RETROACTIVE — 2026-04-30)

### Schema drift fix held

```sql
\d ip_addresses
-- last_seen_at | timestamp with time zone | nullable
```
Column EXISTS. Recovery held since 2026-04-26.

### PAT-023 Systemic Risk Check (spot audit)

| Table | Migrations referencing column-add | All columns present? |
|-------|----------------------------------|-----|
| ip_addresses | 20260424000003_ip_addresses_last_seen | YES (`last_seen_at` present) |
| password_reset_tokens | 20260425000001_password_reset_tokens (full table) | YES (table + 3 indexes present) |
| sims | core_schema + 20260420000002_sims_fk_constraints + multiple | YES (all 24 columns + FKs present, including `fk_sims_ip_address`) |
| sessions | core_schema + protocol_type/slice_info migrations | YES (all 25 columns present, partitioned table intact) |
| alerts | core_schema + 20260423000001_alerts_dedup_statemachine | YES (all 24 columns including dedup_key/cooldown_until/occurrence_count/first_seen_at/last_seen_at present) |

`schema_migrations` reports `version=20260504000002 dirty=f` (consistent with latest migration on disk).
**No drift recurrence detected on the 5 spot-checked tables.**

### Build & Vet

```
go build ./...    → SUCCESS
go vet ./...      → clean (no issues)
```

### Tests

```
go test ./internal/api/sim/... ./internal/store/... -count=1
  → 560 passed in 3 packages

go test ./internal/api/sim/... ./internal/store/... -count=1 -run "Activate|Suspend"
  → 14 passed in 3 packages
```

FIX-253's regression tests (`internal/store/sim_suspend_test.go` 8 cases + handler tests 3 cases) all GREEN.

### Migrations idempotent

```
make db-migrate
  → "migrate: no change — already at latest version"
```

### Live API verify (round-trip on clean SIM)

Original stuck SIM `fffa41ad-abfe-4cd6-9ec6-6bd81409e2c1` was destroyed by the DROP SCHEMA recovery (per AC-5 fallback path: `make db-seed`). Symptom path was reproduced on a fresh admin-tenant SIM `b1b5af33-f97b-4871-afd6-0596f9ef6c61`:

| Action | HTTP | Status |
|--------|------|--------|
| `POST /api/v1/sims/b1b5af33.../suspend` | 200 | success |
| `POST /api/v1/sims/b1b5af33.../activate` | 200 | success |

Original symptom (HTTP 500 on activate) **does not reproduce**.

## Architectural Verdict on db-reset Choice

| Aspect | Verdict |
|--------|---------|
| Right call for dev environment? | YES — schema drift was systemic (2 confirmed missing migrations). Patching individual columns leaves the rest of the drift latent. |
| Risks mitigated? | YES — pre-reset backup `backups/pre-fix252-reset-20260426-204557.sql` (7.1MB) exists; backup is recoverable. |
| Production blast radius? | DIFFERENT — production would NOT permit `DROP SCHEMA CASCADE`. PAT-023 documents this explicitly: in prod, individual `migrate force <version>` reverts + targeted `up` would be required, with downtime and per-table data preservation. |
| Documented as dev-only path? | YES — PAT-023 prevention rule in `bug-patterns.md` line 32; DEV-387 RCA in `decisions.md` line 622. |
| Long-term safety net | Boot-time `schemacheck` is wired in `cmd/argus/main.go` (per closure commit message). Validates DB column inventory on startup; will refuse to boot on drift. |

**Verdict: defensible dev-path recovery; production-safe variant documented in PAT-023.**

## Escalated Issues

None. The original escalation (schema drift) was resolved at closure time; the spinoff (defensive code) shipped as FIX-253 commit `95856fb`.

## Deferred Items

None new in this retroactive Gate. The only "deferral" at original closure was the FIX-253 spinoff which has since CLOSED (2026-04-26).

## Findings (RETROACTIVE)

### F-RETRO-1 | LOW | doc consistency

- Title: Step-log line 8 references `commit=35378db` but actual closure commit is `b5e3ac0`
- Location: `docs/stories/fix-ui-review/FIX-252-step-log.txt:8`
- Description: STEP_5 COMMIT line records hash `35378db`; `git log` shows the only FIX-252 closure commit is `b5e3ac0` (Sun Apr 26 20:59:23 2026 +0300). Likely a copy-paste error in step-log finalization.
- Fixable: YES (minor doc fix; do NOT amend the commit itself)
- Resolution: **DEFERRED** — trivial doc-only inconsistency; no functional impact. Routed to **D-168** below.

## Tech Debt Routed (NEW)

| ID | Source | Description | Target Story | Status |
|----|--------|-------------|-------------|--------|
| D-168 | FIX-252 Retroactive Gate | Step-log line 8 hash typo (`35378db` → should be `b5e3ac0`) | NEXT-DOC-CLEANUP | OPEN |

(Will be added to `docs/ROUTEMAP.md` Tech Debt table.)

## Pass 0 — Maintenance Mode Regression (zero-code, no code change)

| Check | Status |
|-------|--------|
| Existing API endpoints unchanged | YES (no code touched) |
| DB columns preserved | YES (all migrations reapplied cleanly post-reset) |
| Component prop signatures unchanged | YES (FE not touched) |
| Patterns broken | NONE |
| Architecture guard | PASS |

## Passed Items

- Build: `go build ./...` PASS
- Vet: `go vet ./...` PASS (no issues)
- Story tests: `go test -run "Activate|Suspend"` 14/14 PASS
- Full sim+store suite: 560/560 PASS
- Schema verification: `last_seen_at` column present on `ip_addresses`
- PAT-023 spot-check (5 tables): no drift recurrence
- Live API: suspend+activate round-trip 200/200
- Migrations: idempotent (no version drift)
- Docs: USERTEST FIX-252 section + PAT-023 + DEV-386/387/388 all present
- Spinoff verification: FIX-253 closed (commit 95856fb) with 11 regression tests GREEN

## Verdict

**RETROACTIVE GATE: PASS (2026-04-30)**

The schema-drift recovery held; the defensive ACs were addressed via FIX-253 (which shipped successfully); the verification path for the original symptom is reproducible and returns HTTP 200; PAT-023 systemic check shows no drift recurrence. One trivial doc inconsistency (D-168) routed.

The original user-approved Gate bypass on 2026-04-26 was justified given the unique schema-drift scope. This retroactive backfill brings the FIX-252 artifact bundle to standard protocol completeness.
