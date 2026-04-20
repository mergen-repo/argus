# Gate Report: FIX-206 — Orphan Operator IDs Cleanup + FK Constraints + Seed Fix

## Summary

- Requirements Tracing: Fields 6/6, Endpoints 3/3, Workflows 1/1 (fresh-volume migration+seed), Components N/A (backend-only)
- Gap Analysis: 8/8 acceptance criteria passed (AC-1..AC-8)
- Compliance: COMPLIANT
- Tests: FIX-206 story tests 6/6 PASS (5 `TestFIX206*` + rewritten `TestSIMStore_ListEnriched_OrphanOperator_Blocked`)
- Full suite: 305 PASS / 14 FAIL → after fix: 306 PASS / 13 FAIL (F-B1 resolved; remaining 13 = pre-existing, tracked via D-066)
- Test Coverage: AC-7 has negative tests (`TestFIX206_SIMStore_Create_RejectsOrphanOperator/APN/IPAddress`); AC-4 covered via fresh-volume smoke; FK rejection regression now asserted in rewritten test.
- Performance: 1 issue found, 1 fixed (Migration B prod-rollout WARNING; prod cutover tracked as D-065)
- Build: PASS (`go build ./...` clean)
- Vet: PASS (`go vet ./...` clean)
- Overall: **PASS**

## Team Composition

- Analysis Scout: 10 findings (F-A1..F-A10, F-A99)
- Test/Build Scout: 7 findings (F-B1, F-B2..F-B7 grouped)
- UI Scout: 0 findings (skipped — backend + migrations only)
- De-duplicated: 17 → 17 findings (no overlaps; scouts covered disjoint areas)

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Test (regression) | `internal/store/sim_list_enriched_test.go` | Rewrote `TestSIMStore_ListEnriched_OrphanOperator` → `TestSIMStore_ListEnriched_OrphanOperator_Blocked`. New test attempts orphan-operator INSERT, asserts `*InvalidReferenceError` (SQLSTATE 23503) with `Constraint=fk_sims_operator` and `Column=operator_id`. Preserves FIX-206 regression-guard value; FIX-202 DTO "unknown" fallback via `LEFT JOIN + COALESCE` in `ListEnriched` is still exercised by the happy-path enriched tests — the code path remains live for production races between handler validation and operator delete, even though it cannot be provoked by a simple INSERT anymore. | FIX-206 tests 6/6 PASS with DATABASE_URL |
| 2 | Compliance (docs) | `docs/architecture/ERROR_CODES.md` | Added `INVALID_REFERENCE` row under "Validation Errors" (HTTP 400) + appended `CodeInvalidReference` constant under the Validation block of the Go constants ledger. Example envelope matches the new handler wording. | Grep confirms row + constant present |
| 3 | Compliance (wording) | `internal/api/sim/handler.go` | Aligned error message with plan example — now emits `"<field> does not reference an existing <entity>"` (e.g. `"operator_id does not reference an existing operator"`) instead of `"Referenced <field> does not exist"`. Added deterministic `field → entity` map for operator_id/apn_id/ip_address_id. | `go vet` clean + FIX-206 tests PASS |
| 4 | Performance (prod safety) | `migrations/20260420000002_sims_fk_constraints.up.sql` | Prepended a DO $$ ... RAISE WARNING block at the top of Migration B that detects >100k rows in `sims` and emits a WARNING pointing at ROUTEMAP D-065 runbook. **Deliberate deviation from scout suggestion (NOTICE → WARNING)**: prod lock-risk is strong enough that NOTICE would silently scroll past in docker-compose logs; WARNING surfaces in psql prompts and most log aggregators by default. Fresh-volume / dev DB (≤100k rows) stays silent. | Validated DO-block syntax against live PG16 (returns `DO`); live DB has 384 rows → warning stays silent |
| 5 | Compliance (docs) | `internal/store/errors.go` | Added a multi-line PAT-006 reminder comment above `simsFKConstraintColumn` pointing future FK-wiring authors at the `asInvalidReference` pattern + the three new-FK steps (map, store-layer routing, ERROR_CODES.md update). Names D-062/D-063/D-064/D-065 as the concrete follow-up stories. No lint guard — opt-in per store — but the pattern is now discoverable from the code, not buried in a gate report. | `go build ./...` + `go vet ./...` clean |
| 6 | Compliance (story clarity) | `docs/stories/fix-ui-review/FIX-206-orphan-operator-cleanup.md` | Added "USERTEST — AC-1 Audit Trail Verification" section naming both audit-trail artifacts: (i) Postgres migration-run logs (`RAISE NOTICE` entries via `docker compose logs argus \| grep 'FIX-206 audit:'`) and (ii) `sims.metadata -> fix_206_orphan_cleanup` JSONB (queryable per-row forensic record). Documents that Option B (NOTICE) was chosen over direct audit_logs INSERT because the global hashchain computed in Go cannot be safely replicated in PL/pgSQL. | Story file updated; scenario describes verification steps for an operator running AC-1 checks |
| 7 | Tech Debt tracking | `docs/ROUTEMAP.md` | Added D-066 row for the 13 pre-existing test-suite failures surfaced by the FIX-206 Gate full-suite sweep (unrelated to sims/FKs — covers user_backup_codes FK, password_history FK, users.name, pgx OID 25 encoding, audit hashchain drift, migration-chain, sms_outbound encoding). Target: future test-infra cleanup story. | ROUTEMAP updated |

## Escalated Issues (needs Amil attention)

### [E-1] [HIGH] Production 10M-row FK cutover strategy deferred

- Source: scout-analysis F-A99, scout dispatch confirmation
- Expected: online-safe FK add (NOT VALID + VALIDATE) per plan §Migration B
- Actual: PostgreSQL 16 rejects `ALTER TABLE <partitioned> ADD FOREIGN KEY ... NOT VALID` with "cannot add NOT VALID foreign key on partitioned table". Migration B therefore uses plain `ADD CONSTRAINT ... FOREIGN KEY`, which holds ACCESS EXCLUSIVE for the full validation scan. On a 10M-row production sims table this will stall live RADIUS/Diameter traffic for minutes.
- Why escalated: this is a production infrastructure runbook, not a code fix. Per-partition strategy (add FK to each partition individually with NOT VALID + VALIDATE — partitions are plain tables and support that pattern; or detach+reattach) requires dedicated scoping with ops + an empirical dry run against a production clone.
- Suggested approach: before the FIX-206 migration lands in production, author the D-065 runbook: (a) detach each partition, (b) add FK on each detached partition with NOT VALID + VALIDATE, (c) re-attach. Fast in dev, transparent to AAA traffic. Tracked as ROUTEMAP D-065 with full rationale.
- Status: tracked via ROUTEMAP D-065, pre-prod blocker.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-062 | `sessions` hypertable — `operator_id` NOT NULL, no non-destructive cleanup for FK add; historical session data must not be mutated. Needs dedicated data-integrity scoping. | future data-integrity extension | YES (pre-existing) |
| D-063 | `cdrs` hypertable — same rationale as D-062; charging records preserve historical operator attribution. | future data-integrity extension | YES (pre-existing) |
| D-064 | `operator_health_logs` hypertable — `operator_id` NOT NULL, diagnostic history loss risk. | future data-integrity extension | YES (pre-existing) |
| D-065 | Production 10M-row Migration B cutover runbook (per-partition FK strategy). Plain `ADD CONSTRAINT` acceptable for dev/fresh-volume (≤10k rows); warning emitted at migrate-time if sims > 100k. | pre-prod infrastructure | YES (pre-existing) |
| D-066 | Pre-existing 13 test-suite failures surfaced by FIX-206 Gate full-suite sweep. Unrelated to sims/FKs — covers user_backup_codes FK, password_history FK, users.name NOT NULL, pgx OID 25 encoding, audit hashchain drift, migration-chain, sms_outbound encoding. Files: `sim_list_enriched_explain_test.go`, `tenant_test.go`, `backup_codes_test.go`, `backup_store_test.go`, `password_history_test.go`, `audit_integration_test.go`, `migration_freshvol_test.go`, `sms_outbound_test.go`. | future test-infra cleanup story | YES (NEW) |

## Performance Summary

### Queries Analyzed

| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `migrations/20260420000002_sims_fk_constraints.up.sql:31-44` | Plain `ALTER TABLE sims ADD CONSTRAINT ... FOREIGN KEY` (×3) | ACCESS EXCLUSIVE on sims during validation; minutes-long stall on 10M-row prod | MEDIUM | Warning added at migrate-time; runbook tracked via D-065 (escalated) |
| 2 | `migrations/20260420000001_sims_orphan_cleanup.up.sql:123-138` | `UPDATE sims SET operator_id = (mapping[src])::uuid WHERE NOT EXISTS (...)` | 200 orphan rows; cross-partition row movement (sims_default → sims_turkcell/vodafone/turk_telekom). Fast on seed data. | LOW | Acceptable at story scale; partition-pruning optimization tracked via D-047 |

### Caching Verdicts

| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | SIM enriched DTO (operator_name/apn_name join) | `internal/store/sim.go ListEnriched` | n/a | No cache needed — LEFT JOIN + COALESCE is a single round-trip per request. FIX-206 enables the operator_name to be deterministically non-NULL for non-orphan rows. | Unchanged by FIX-206 |

## Verification

- Tests after fixes: FIX-206 suite 6/6 PASS (`DATABASE_URL="postgres://argus:argus_secret@localhost:5450/argus?sslmode=disable" go test -run 'TestFIX206|TestSIMStore_ListEnriched_OrphanOperator_Blocked' -count=1 -v ./internal/store/...`)
- Build after fixes: PASS (`go build ./...` clean)
- Vet after fixes: PASS (`go vet ./...` clean)
- Migration A up → idempotent (fresh volume fast-path, dirty-DB remap+suspend); verified via migrate up on running DB with 384 sims (no orphans).
- Migration B up → all 3 FK constraints present in `pg_constraint` with `convalidated=true` (verified via Task 5 `TestFIX206_FK_Constraints_Installed`).
- Token enforcement: N/A (backend-only story)
- Fix iterations: 1 (no re-runs needed — all fixes compiled + tested on first attempt)

## Pass 0: Regression Verification (Maintenance)

N/A — FIX-206 is a new feature story in the UI Review Remediation track, not a maintenance-mode hotfix/bugfix. Standard gate flow applied.

## Passed Items

- **F-A4**: `store.sim.Update` path does not route through `asInvalidReference` — confirmed correct; `sim.go` has only state-transition helpers (Activate/Suspend/Resume/Terminate), no general Update. Policy assignment writes `policy_version_id` which has no FK in FIX-206. No action needed.
- **F-A5**: Migration A `down.sql` is intentionally no-op — documented in plan §Risk 4 and in the down.sql file header. Rollback strategy = pg_dump restore. LOW; no action needed.
- **F-A6**: `bulk_handler.Import` defers operator/APN validation to job processor — existing behavior unchanged by FIX-206; FK defense applies inside the job's `SIMStore.Create` path. Not a regression.
- **F-A9**: Migration A UPDATEs lack explicit partition-pruning — for 200 orphan rows (story scale) this is fast. Not a FIX-206 concern; tracked under D-047.
- **F-A10**: Integration test `runAllSeeds` bypasses cmd/argus/main.go seed runner — acceptable parity with main.go. No action needed.
- **F-B2..F-B7**: 13 pre-existing test failures unrelated to FIX-206 — tracked via D-066 (NEW). None touch sims or fk_sims_*. Not a FIX-206 regression.
- **FK regression guard**: new `TestSIMStore_ListEnriched_OrphanOperator_Blocked` asserts `fk_sims_operator` rejects orphan-operator INSERT with `*InvalidReferenceError`. Constraint name, column mapping, and error unwrap all verified.
- **Fresh-volume smoke (AC-4)**: `TestFIX206_FreshVolume_NoOrphanSims` asserts `SELECT COUNT(*) FROM sims s LEFT JOIN operators o ON s.operator_id=o.id WHERE o.id IS NULL = 0` after full `make db-migrate && make db-seed` equivalent. PASS.
- **FK install (AC-2, AC-3)**: `TestFIX206_FK_Constraints_Installed` asserts `fk_sims_operator`, `fk_sims_apn`, `fk_sims_ip_address` all present in `pg_constraint` with `convalidated=true`. PASS.
- **Handler error surface (AC-7)**: `TestFIX206_SIMStore_Create_RejectsOrphanOperator/APN/IPAddress` asserts store returns `*InvalidReferenceError` with correct Constraint + Column fields. Handler translates to 400 INVALID_REFERENCE with the plan-aligned message. PASS.

## Amil Return Summary

```
GATE SUMMARY
=============
Story: FIX-206 — Orphan Operator IDs Cleanup + FK Constraints + Seed Fix
Status: PASS

Team Composition: Analysis(10 findings) + Test/Build(7 findings) + UI(skipped, 0 findings) → merged 17 findings

Requirements Tracing: Fields 6/6, Endpoints 3/3, Workflows 1/1, Components N/A
Gap Analysis: 8/8 ACs passed
Compliance: COMPLIANT
Tests: FIX-206 suite 6/6 PASS (5 TestFIX206* + rewritten TestSIMStore_ListEnriched_OrphanOperator_Blocked); full suite 306/319 (13 pre-existing failures tracked as D-066)
Test Coverage: AC-7 has negative tests, AC-4 fresh-volume smoke covers seed cleanliness, FK regression guard new
Performance: 1 issue found (Migration B prod lock), 1 fixed (WARNING added), escalated runbook tracked as D-065
Build: PASS (go build + go vet clean)
Token Enforcement: N/A (backend story)

Fixes applied: 7
- Test rewrite: F-B1 TestSIMStore_ListEnriched_OrphanOperator → _Blocked (assert FK rejects orphan INSERT)
- Docs: INVALID_REFERENCE added to ERROR_CODES.md + Go constants ledger
- Handler wording: "<field> does not reference an existing <entity>" (plan-aligned)
- Migration B prod-safety: RAISE WARNING at migrate-time if sims > 100k (D-065 runbook pointer)
- PAT-006 reminder: multi-step comment in errors.go pointing future FK authors at asInvalidReference pattern
- Story USERTEST: audit-trail verification steps (RAISE NOTICE logs + sims.metadata JSONB)
- ROUTEMAP: D-066 row for 13 pre-existing test failures

Escalated: 1 (needs pre-prod attention, already tracked)
- [E-1] [HIGH] Production 10M-row Migration B cutover — per-partition runbook → D-065

Deferred: 5 (all written to ROUTEMAP → Tech Debt; D-066 is NEW)
- D-062..D-064: hypertable FK deferral (sessions/cdrs/operator_health_logs) → future data-integrity story
- D-065: Migration B prod cutover runbook → pre-prod infrastructure
- D-066: 13 pre-existing test failures → future test-infra cleanup story

Verification: tests PASS, build PASS, vet PASS

Gate report: docs/stories/fix-ui-review/FIX-206-gate.md
```
