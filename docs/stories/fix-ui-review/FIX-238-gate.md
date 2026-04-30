# Gate Report: FIX-238 — Remove Roaming Feature (full stack, L)

## Summary
- Requirements Tracing: ACs 10/10, Endpoint removals 6/6, FE removals 8/8, Components 2/2
- Gap Analysis: 10/10 acceptance criteria PASS (after Gate doc cleanup)
- Compliance: COMPLIANT
- Tests: full suite 3803 passed (notification subset re-run 77/77 PASS after comment fix)
- Test Coverage: AC-10 archiver covered by 3 DB-gated tests; AC-3/4/5/6/8 covered by full regression
- Performance: no new issues (boot-time archiver bounded, idempotent)
- Build: PASS (`go build` 0 errors, `go vet` clean, `tsc --noEmit` 0 errors, `vite build` ~2.56s)
- Screen Mockup Compliance: SCR-150 / SCR-151 retired (intentional)
- UI Quality: PASS — 0 findings (UI scout)
- Token Enforcement: not in scope (no UI surface added)
- Turkish Text: not in scope
- Overall: **PASS**

## Team Composition
- Analysis Scout: 8 findings (F-A1..F-A8) — 4 fixable (F-A1..F-A4), 4 INFO/verification (F-A5..F-A8)
- Test/Build Scout: 0 findings (PASS)
- UI Scout: 0 findings (PASS) — out-of-scope note about SCR-150/151 docs forwarded into F-A1
- De-duplicated: 8 → 4 actionable findings (INFO findings retained as PASS evidence)

## Fixes Applied

| #  | Category   | File                                                          | Change                                                                                                                                                                                                                | Verified |
|----|------------|---------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|----------|
| 1  | Compliance | docs/architecture/api/_index.md:394-403                       | Replaced API-230..API-235 table with REMOVED block (FIX-238 changelog note); IDs retired                                                                                                                              | grep clean |
| 2  | Compliance | docs/architecture/ERROR_CODES.md:196                          | Stripped `roaming` from DSL_COMPILE_ERROR example "Supported" list                                                                                                                                                    | grep clean |
| 3  | Compliance | docs/architecture/ERROR_CODES.md:514                          | Removed `+ job/roaming_renewal.go` from operator scope description                                                                                                                                                     | grep clean |
| 4  | Compliance | docs/architecture/ERROR_CODES.md:529                          | Removed `roaming.agreement.renewal_due` row from publisher source map                                                                                                                                                  | grep clean |
| 5  | Compliance | docs/architecture/CONFIG.md:119                               | Stripped "roaming renewal" from `argus.events.alert.triggered` description                                                                                                                                            | grep clean |
| 6  | Compliance | docs/architecture/CONFIG.md:286-291                           | Replaced `Roaming Agreements (SVC-06) — STORY-071` env var section with REMOVED block (FIX-238 changelog)                                                                                                              | grep clean |
| 7  | Compliance | docs/architecture/CONFIG.md:623-625                           | Removed `# === Roaming Agreements ===` block from sample env file                                                                                                                                                      | grep clean |
| 8  | Compliance | docs/architecture/WEBSOCKET_EVENTS.md:96                      | Removed `roaming.agreement.renewal_due` from in-scope subjects list                                                                                                                                                    | grep clean |
| 9  | Compliance | docs/architecture/EVENTS.md:100                               | Removed `roaming.agreement.renewal_due` Tier 3 entry                                                                                                                                                                  | grep clean |
| 10 | Compliance | docs/architecture/db/_index.md:77                             | Marked TBL-43 with `~~TBL-43~~ (REMOVED FIX-238)` annotation                                                                                                                                                          | grep clean |
| 11 | Compliance | docs/architecture/DSL_GRAMMAR.md:117 / :125 / :141 / :170-172 / :275-280 | Removed `roaming` MATCH-field row, `roaming` WHEN-condition row, both DSL examples, and the "Roaming restrictions" example block; added a deprecation note pointing at the boot-time archiver | grep clean |
| 12 | Compliance | docs/migrations-notes.md:7 + docs/PRODUCT.md:479              | Fixed migration filename: `20260430_drop_roaming_agreements` → `20260505000001_drop_roaming_agreements`                                                                                                                | grep clean |
| 13 | Compliance | docs/SCREENS.md:53-54 + header note + total-count line        | Marked SCR-150 / SCR-151 rows as REMOVED FIX-238 (IDs retired); updated header total to 81 (was 83) and revised reservation note                                                                                       | grep clean |
| 14 | Compliance | docs/USERTEST.md STORY-071 block (lines 1640-1676) + line 5176 | Replaced 19-scenario STORY-071 user-test block with single REMOVED note pointing at FIX-238; stripped `roaming` from policy editor autocomplete vocab expectation (Senaryo 4)                                          | grep clean |
| 15 | Gap        | internal/api/roaming/                                         | `rmdir` empty directory remnant (cosmetic — Go ignores empty dirs but project hygiene)                                                                                                                                 | dir gone |
| 16 | Gap        | internal/notification/service_test.go:1453                    | Removed `roaming_renewal` from comment list of tested publisher scenarios; updated phrasing from "Five of seven publishers" → "Several publishers"                                                                     | go test PASS |

> Total: 16 single-writer edits across 14 files (Gate Lead phase only — original W1..W5 dev work is upstream).

## Escalated Issues
**None.** All findings were fixable doc/test cleanup.

## Deferred Items
**None.** No tech-debt rows added; nothing punted to a future story.

> Note (dispatch reconciliation): the dispatch headline mentioned "4 `CodeRoamingAgreement*` error codes at ERROR_CODES.md:529". On inspection, line 529 was a single publisher source-map row (`roaming.agreement.renewal_due | operator | internal/job/roaming_renewal.go`). There were no `CodeRoamingAgreement*` constants in the doc. Fixed what was actually there (the single row + the line 196 example + the line 514 scope description) rather than fabricate four non-existent constants.

> Scope expansion: per advisor reconcile, the Gate Lead also fixed stale references the dispatch list under-counted: DSL_GRAMMAR.md (5 lines + example block), CONFIG.md:119 + sample env file block, ERROR_CODES.md:196 + :514, and the out-of-scope SCREENS.md / USERTEST.md cleanup. This prevents F-A1 reopening on the next sweep.

## Performance Summary

### Queries Analyzed
| # | File:Line | Pattern | Issue | Severity | Status |
|---|-----------|---------|-------|----------|--------|
| 1 | roaming_keyword_archiver.go:38-44 | SELECT join with `ILIKE %roaming%` | Boot-time one-shot, no index needed | LOW (acceptable) | accepted |
| 2 | roaming_keyword_archiver.go:64-67 | per-row UPDATE in loop | Bounded by SELECT (typically <100 rows), idempotent | LOW (acceptable) | accepted |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| 1 | Archiver result | none | N/A | SKIP — boot-only one-shot, idempotent | accepted |

## Token & Component Enforcement
Not applicable (FIX-238 is removal-only — no UI surface added).

## Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS (0 errors) |
| `go vet ./...` | PASS (no issues) |
| `go test ./internal/notification/...` | PASS (77/77 — comment-only fix did not affect runtime) |
| `pnpm tsc --noEmit` | PASS (0 errors) |
| `ls internal/api/roaming` | "No such file or directory" (correct) |
| `grep roaming` over docs | only intentional REMOVED markers + migration ops note + archiver code path |
| Fix iterations | 1 (no re-fixes required) |

Full Go-test suite was already validated at 3803/3803 by the Test/Build scout pre-Gate. Gate-phase doc edits cannot affect Go test outcomes; the targeted notification subset re-run (after the test comment cleanup) confirmed no regression.

## Maintenance Mode — Pass 0 Regression
Not applicable (FIX-238 is a feature-removal story, not maintenance/bugfix).

## Passed Items
- AC-1 FE removal (UI scout: 0 findings; routes 404; sidebar reduced; alias `?tab=agreements → overview` verified)
- AC-2 BE removal (zero `roaming` symbols in active Go code outside the intentional archiver)
- AC-3 DSL grammar removal (parser + evaluator + tests cleaned; `validMatchFields` no longer contains `roaming`)
- AC-4 DB drop migration (SQL roundtrip verified via psql in transaction with ROLLBACK; idempotent `IF EXISTS CASCADE`)
- AC-5 Config cleanup (`RoamingRenewalAlertDays` / `RoamingRenewalCron` removed from `internal/config/config.go`)
- AC-6 Test cleanup (bench `Field: "roaming"` → `max_sessions`; service_test comment cleaned in this Gate)
- AC-7 Cross-ref removal (after Gate doc cleanup — 6 architecture sub-docs + SCREENS + USERTEST fixed)
- AC-8 Regression gate (3803 Go tests, vet clean, tsc 0, vite 2.56s)
- AC-9 Migration safety (filename in migrations-notes + PRODUCT.md fixed; `IF EXISTS CASCADE` confirmed)
- AC-10 Archiver (idempotent, audit-logged, error-resilient; 3 DB-gated tests; main.go:521 boot wiring)
- Risk 1 SoR cost optimization preserved (`engine.go` `ReasonCostOptimized` + `CostPerMB` intact)
- Risk 2 Existing DSL with roaming keyword auto-archived at boot (AC-10 archiver)
- Risk 3 URL alias `?tab=agreements → overview` verified (`use-tab-url-sync.ts` + `detail.tsx:1093`)
- PAT-026 6-layer sweep verified clean (L1 handler, L2 store, L3 DB, L4 seed, L5 job, L6 main wiring) + extended L7/L8 (event catalog, publisher source map)
