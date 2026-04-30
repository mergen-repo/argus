# Gate Report: FIX-207 — Session/CDR Data Integrity

## Summary
- Requirements Tracing: AC 7/7 addressed (AC-1 Option B, AC-2 CHECK, AC-3 framed_ip now wired, AC-4 IMSI validator × 4 sites, AC-5 daily scan, AC-6 session_quarantine, AC-7 NAS-IP signal helper)
- Gap Analysis: 7/7 acceptance criteria PASS (AC-3 elevated from partial → PASS after Gate-applied main.go wiring)
- Compliance: COMPLIANT (error envelope, tenant scoping, migration ordering, hypertable CHECK, PAT-006/PAT-009 pattern warnings respected)
- Tests: full suite 3381 passed / 0 failed (pre-gate 3378) across 103 packages; affected packages 786 passed / 0 failed
- Test Coverage: 7/7 ACs with at least one positive + negative test (AC-3 still lacks full DB integration but unit coverage present; AC-1/AC-2 now have Go-layer DB-gated probe in addition to live-psql verification)
- Performance: 8 queries analyzed, bounded + chunk-pruned; 3 caching decisions confirmed (no new hot-path regressions introduced by Gate fixes)
- Build: PASS (`rtk go build ./...`)
- Vet: PASS (`rtk go vet ./...`)
- Race: PASS on affected packages (`-race` no data races)
- Overall: PASS

## Team Composition
- Analysis Scout: 12 findings (F-A1 .. F-A12)
- Test/Build Scout: 4 findings (F-B1 .. F-B4)
- UI Scout: skipped (backend-only story)
- De-duplicated: F-A3 + F-B3 (same helper-level-test issue) merged → 15 unique findings
- Post-merge severity: 1 CRITICAL, 3 HIGH, 7 MEDIUM, 4 LOW

## Fixes Applied
| # | Finding | Category | File | Change | Verified |
|---|---------|----------|------|--------|----------|
| 1 | F-A1 | gap (CRITICAL) | `cmd/argus/main.go` (3 call sites: ~:956 RADIUS, ~:1027 Diameter, ~:1061 SBA) | Added `WithIPPoolStore(ippoolStore)` + `WithMetrics(metricsReg)` to all `aaasession.NewManager` invocations. Lifts AC-3 validation from runtime no-op to actually running in the binary. | `go build ./...` PASS; `go vet ./...` PASS; tests PASS |
| 2 | F-A2 | gap (HIGH) | `internal/job/data_integrity_test.go` | Added `TestDataIntegrityDetector_Run_ReportsFramedIPOutsidePool` — 4 violations seeded via `queryRowValues[0]=4`, asserts WARN log, metric `argus_data_integrity_violations_total{kind="framed_ip_outside_pool"} 4`, and counts-map in job result. | `rtk go test -count=1 ./internal/job/...` PASS |
| 3 | F-A3 + F-B3 | gap / test-quality (HIGH + LOW merged) | `internal/aaa/radius/nas_ip_test.go` | Renamed `TestHandleAcctStart_PersistsNASIP` → `TestExtractNASIPFromPacket_ReturnsIP` and `TestHandleAcctStart_MissingNASIP_EmitsSignal` → `TestExtractNASIPFromPacket_MissingAVP_EmitsSignal`. Added doc-comment acknowledging helper-level scope and pointing to D-071 for end-to-end persistence coverage. | `rtk go test ./internal/aaa/radius/...` PASS |
| 4 | F-B1 | lint (MEDIUM) | `internal/job/data_integrity.go` | Removed 2 `TODO(FIX-207 followup)` comments; reworded surrounding prose to reference D-069 (sims-quarantine surface) and D-070 (per-tenant notification wiring) in ROUTEMAP. | `rtk go vet ./...` PASS; lint claim now honest |
| 5 | F-B1 | lint (MEDIUM) | `docs/ROUTEMAP.md` | Added rows D-069, D-070, D-071 to Tech Debt table with target stories and rationale. | Grep: `D-069`, `D-070`, `D-071` present |
| 6 | F-B2 | test-coverage (MEDIUM) | `internal/store/session_cdr_invariants_check_test.go` (new) | DB-gated test `TestSessionCDRInvariants_CHECKConstraints_RejectsBadInserts` with 4 subtests: sessions rejects `ended_at<started_at` with SQLSTATE 23514 + constraint name `chk_sessions_ended_after_started`; cdrs rejects `duration_sec<0` with 23514 + `chk_cdrs_duration_nonneg`; sessions accepts `ended_at=NULL` happy path; cdrs accepts `duration_sec=0` boundary. Reuses `testIPPoolPool` helper which skips when `DATABASE_URL` is unset. | `rtk go test ./internal/store/...` PASS (DB-gated tests skip cleanly in CI without DB) |
| 7 | F-A5 | compliance (MEDIUM) | `docs/stories/fix-ui-review/FIX-207-session-cdr-data-integrity.md` | Appended "Post-migration reconciliation" section with both reconciliation paths (re-run `make db-migrate` OR insert `schema_migrations` rows directly). Added "Gate follow-ups" section documenting the 7 gate fixes + 4 deferred items. | Story file updated |
| 8 | F-A8 | gap (MEDIUM) | `internal/aaa/validator/imsi_test.go` | Added 2 subtests: `tab-padded strict rejects` and `unicode-digit strict rejects`. Closes the 15-vs-17 subtest count drift. | `go test -v ./internal/aaa/validator/...` — 13 subtests in `TestValidateIMSI` (was 10) + 5 in `TestIsIMSIFormatValid` = 18 subtests total. Total `-v` PASS line count increased by 2. |

## Deferred Items (written to ROUTEMAP → Tech Debt)
| # | Finding | Rationale | Target Story | Written to ROUTEMAP |
|---|---------|-----------|--------------|---------------------|
| D-067 | F-A5 parent (pre-existing) | Migration B plain-CHECK prod cutover runbook | future pre-prod infra | YES (pre-existing from dev step) |
| D-068 | F-A12 (conditional) | framed_ip validation hot-path cache — accept until post-dev benchmark flags >2ms | POST-GA perf | NOT YET ADDED (conditional per plan; open only if benchmark exceeds threshold) |
| D-069 | F-B1 | Sims-quarantine surface for IMSI violations (session_quarantine.original_table CHECK excludes 'sims') | future data-integrity extension | YES (new row) |
| D-070 | F-B1 | Per-tenant notification-store wiring in DataIntegrityDetector | future observability | YES (new row) |
| D-071 | F-A3/F-B3 | DB-gated integration test for full RADIUS → sessionStore NAS-IP persistence | future test-infra | YES (new row) |

## Deferred Items (Accepted without ROUTEMAP)
| # | Finding | Rationale |
|---|---------|-----------|
| F-A4 | Diameter/SBA IMSI coverage | Plan explicitly scoped to RADIUS auth/acct + SIM handler + CDR consumer. Diameter/SBA belong to a dedicated story when those initiatives are scoped. Story closure note added. |
| F-A6 | audit-log wiring for framed_ip mismatch | Documented deviation in story; WARN log + metric meets ops-visible bar. Can be wired via `WithAuditor` option in a follow-up; no user-visible regression. |
| F-A7 | notification-store wiring for scan job | Subsumed by D-070. |
| F-A9 | SIM handler empty-IMSI returns 422 not 400 | Intentional asymmetry; 422 is correct for semantic validation failure when field is present-but-empty vs 400 for strict format mismatch. ERROR_CODES.md is already consistent. |
| F-A10 | session_quarantine.original_table CHECK excludes 'sims' | Subsumed by D-069. |
| F-A11 | Daily scan IMSI regex unbounded | Accepted — daily frequency + ~10k sims per tenant dev volume makes full scan a non-issue; can index `imsi` if cron frequency increases. |
| F-A12 | framed_ip validation DB round-trip | Accepted — now relevant after F-A1 fix wired the path; benchmark will be captured in next perf sweep; track D-068 conditionally. |
| F-B4 | macOS linker warnings under `-race` | Benign toolchain noise; no action needed. |

## Escalated Issues
None. All CRITICAL + HIGH findings were fixable within story scope.

## Performance Summary

### Queries Analyzed (from scout)
| # | Location | Pattern | Status |
|---|----------|---------|--------|
| 1 | `data_integrity.go` qNegSessionSQL | INSERT … WHERE ended_at<started_at AND started_at >= NOW()-24h | BOUNDED |
| 2 | `data_integrity.go` qNegCDRSQL | INSERT … WHERE duration_sec<0 AND timestamp >= NOW()-24h | BOUNDED |
| 3 | `data_integrity.go` qIPOutsideSQL | SELECT COUNT(*) with NOT EXISTS join, apn_id guard | BOUNDED, nullable-FK-safe |
| 4 | `data_integrity.go` qIMSISQL | SELECT COUNT(*) FROM sims WHERE imsi !~ '^\d{14,15}$' | UNBOUNDED (intentional — structural check) |
| 5 | `session.go` validateFramedIP pool lookup | ListByAPN — APN-scoped, tenant-bounded | BOUNDED; now actually runs (F-A1 fix) |
| 6 | Migration B `ALTER TABLE ... ADD CONSTRAINT` | Full-chunk scan under ACCESS EXCLUSIVE | D-067 tracks prod runbook |
| 7 | `ip_pools` APN lookup | WHERE tenant_id + apn_id + state=active (indexed) | BOUNDED |
| 8 | Migration A retro INSERT … WHERE … NOT EXISTS | Idempotency guard adds one sub-select per row; 131 bad rows on dev | ACCEPTABLE |

### Caching Verdicts (no change from scout)
| # | Data | Decision |
|---|------|----------|
| 1 | SIM cache in RADIUS | Existing Redis cache sufficient |
| 2 | APN→pool_cidrs hot-path cache | DEFER (D-068 conditional) |
| 3 | IMSI regex | Compiled once at package init (regexp.MustCompile) |

## Verification
- Pre-fix tests: 3378 pass (scout baseline)
- Post-fix tests: 3381 pass (3 new subtests, 0 failures)
- Affected-packages `-race`: PASS (no data races in `internal/aaa/validator`, `internal/job`, `internal/aaa/radius`)
- Build after fixes: PASS (`rtk go build ./...`)
- Vet after fixes: PASS (`rtk go vet ./...`)
- Fix iterations: 1 (no secondary breakage; no reverts needed)

## Passed Items (new or newly-verified by Gate)
- AC-3 `framed_ip` pool validation: now actually wired at all 3 NewManager call sites (RADIUS, Diameter, SBA). F-A1 fix lifts this from "runtime no-op despite complete code path" to "active validation with WARN log + metric". Verified by `rtk go build ./...` and code inspection.
- AC-5 scan job: full test coverage for all 4 invariant kinds (added `TestDataIntegrityDetector_Run_ReportsFramedIPOutsidePool`).
- AC-1/AC-2 CHECK constraints: Go-layer probe added (`TestSessionCDRInvariants_CHECKConstraints_RejectsBadInserts`) — complements the live-psql verification previously recorded in step log. Four subtests covering both rejection and boundary-acceptance paths.
- AC-7 NAS-IP: renamed tests to honestly reflect helper-level scope; D-071 tracks the missing DB-gated E2E path.
- Post-migration reconciliation: documented in story file so ops don't hit a silent `schema_migrations` drift on upgrade.

## Gate Verdict
**PASS** — all MUST-FIX (CRITICAL + HIGH) findings fixed; MEDIUM findings fixed or tracked; LOW findings accepted or subsumed by tracked debt. No escalations. Build/vet/tests/race all PASS. Story is cleared for Commit.
