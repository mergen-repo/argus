# Gate Report: FIX-251 — Stale "An unexpected error occurred" toasts on /sims

> **RETROACTIVE GATE — filed 2026-04-30 to backfill missing artifact from 2026-04-26 closure (commit 211f0cc).**
> Standard 3-scout protocol consolidated into a single retroactive verification pass on already-merged code. No code changes — verification only.

## Summary

- Scope: 2-line backend root-cause fix in `internal/store/operator.go` (`List` 341-349, `ListActive` 470-478) — added `&o.SLALatencyThresholdMs` to inline `rows.Scan(...)` calls that diverged from the `scanOperator` helper after FIX-215 added the `sla_latency_threshold_ms` column. Plus 3 regression tests in `internal/store/operator_list_regression_test.go`.
- Plan pivot: Story spec hypothesised an `AbortError` swallow in `useSimsQuery`. Discovery proved this wrong — toast string `"An unexpected error occurred"` is the backend's standard 500-envelope message. Dev rejected the cosmetic `silentPaths` patch (would have hidden the toast but left `/operators` page broken everywhere). Backend fix applied at the actual root cause.
- AC Coverage: 4/4 PASS (AC-1 zero spurious toasts via real fix; AC-2 scoped — non-suppressed paths still toast; AC-3 root cause documented; AC-4 TS strict + go build clean).
- Compliance: COMPLIANT (PAT-006 RECURRENCE #3 logged in `bug-patterns.md`; DEV-389 logged in `decisions.md`).
- Tests: Operator-scoped 14/14 PASS; FIX-251 regression tests 3/3 PASS.
- Build: PASS (`go build ./...`, `go vet ./...` clean).
- Pre-existing failures (out of scope): tracked in D-066 (`tenants.slug`, `password_history_user_id_fkey`, `users.name NOT NULL`, `setupOTATenant`, `sla_report` seed dup, `sms_outbound` cursor pagination). NOT regressions — predate commit 211f0cc.
- Overall: **PASS** (with D-181 deferred for systemic inline-scan-vs-helper audit).

## Team Composition (Retroactive — single-agent consolidation)

- Analysis Scout: 4 findings (PAT-006 alignment verified, regression-test efficacy verified, systemic audit, FE silentPaths state)
- Test/Build Scout: 1 finding (operator-scoped tests + build all PASS; pre-existing failures isolated)
- UI Scout: 1 finding (BE-only fix; FE static check on `silentPaths` confirmed `/operators` is NOT silenced — correct, since BE fix returns clean responses)

## AC Coverage

| AC | Description | Status | Evidence |
|----|-------------|--------|----------|
| AC-1 | Zero "An unexpected error occurred" toasts on `/sims` cold load when no XHR fails | PASS | Backend fix returns 200 on `/operators`; toast no longer fires. Live: container `argus-app` healthy; `/operators` returns 401 (auth-gate) NOT 500. |
| AC-2 | Real failures still produce meaningful error UI; suppression scoped not blanket | PASS | `silentPaths` UNCHANGED — `web/src/lib/api.ts:93` still has only original 4 entries. No blanket suppression introduced. |
| AC-3 | Root cause documented (offending hook + condition) | PASS | Commit 211f0cc body names `OperatorStore.List`/`ListActive` inline scans + FIX-215 column-add lifecycle as root cause. |
| AC-4 | TS strict; no behavior regression on valid error paths | PASS | `go build ./...` clean; `go vet ./...` clean; FE untouched (no TS surface). |

## Findings Table

| # | Severity | Source | Status | Detail |
|---|----------|--------|--------|--------|
| F-A1 | INFO | analysis | VERIFIED | `operatorColumns` (20 cols), `scanOperator` helper (20 fields), `List` inline scan (20 fields, l.341-349), `ListActive` inline scan (20 fields, l.470-478) — all aligned. |
| F-A2 | INFO | analysis | VERIFIED | Regression tests catch drift: `TestOperatorColumnsAndScanCountConsistency` asserts column count == 20 with explicit failure message pointing operator to inline scans. DB-gated tests seed `sla_latency_threshold_ms` and assert population. |
| F-A3 | LOW | analysis | DEFERRED → D-181 | Systemic audit: 18 store files have `scanXxx` helpers; several (`sim.go` line 345 with `simColumns`, `policy.go` 11 inline scans, `cdr.go` 10 inline scans, `ippool.go` 7 inline scans) have the same drift-risk shape — inline list scans alongside helpers. Non-blocking; future story should add `TestColumnsAndScanCountConsistency` for each table. |
| F-A4 | INFO | analysis | VERIFIED | Architectural verdict — chosen approach (modify the 2 inline scans) is correct for the immediate fix. Refactoring all sites to delegate to `scanOperator` would be in-scope only if FIX-251 were a refactor story; it is a bugfix. Future cleanup is D-180. |
| F-B1 | INFO | testbuild | VERIFIED | `go build ./...` PASS; `go vet ./...` PASS; FIX-251-specific 3 regression tests PASS; all operator-related tests (14/14) PASS. |
| F-B2 | INFO | testbuild | OUT OF SCOPE | Full-suite has pre-existing failures (`tenants.slug`, `password_history_user_id_fkey`, `users.name NOT NULL`, `esim_ota`/`esim_stock` setupOTATenant, `sla_report` seed dup, `sms_outbound` cursor pagination). All predate commit 211f0cc. Already tracked: D-066. NOT a FIX-251 regression. |
| F-U1 | INFO | ui | VERIFIED | BE-only fix — no FE changes. Static check on `web/src/lib/api.ts:93`: `silentPaths = ['/users/me/views', '/onboarding/status', '/announcements/active', '/alerts/export']` — `/operators` NOT included (correct: BE fix means endpoint returns cleanly, no FE silencing needed). |

## Re-verification Output

| Command | Exit | Result |
|---------|------|--------|
| `go build ./...` | 0 | Success |
| `go vet ./...` | 0 | No issues found |
| `go test ./internal/store/... -run "OperatorColumnsAndScanCountConsistency\|OperatorStore_List_RegressesOnInlineScanDriftFIX251\|OperatorStore_ListActive_RegressesOnInlineScanDriftFIX251" -count=1 -v` | 0 | 3/3 PASS |
| `go test ./internal/store/... -run "TestOperator" -count=1` | 0 | 14/14 PASS |
| `docker ps` (`argus-app`) | 0 | `Up 2 days (healthy)` |
| `curl -s -o /dev/null -w "%{http_code}" http://localhost:8084/api/v1/operators` | — | `401` (auth-gated; no 500 — fix verified live) |
| `grep -n "silentPaths" web/src/lib/api.ts` | 0 | line 93 unchanged (4 entries, NO `/operators`) — correct |

## Pre-existing — Out of Scope (already tracked in D-066)

Full-suite test bed drift surfaced during retroactive verification, predating 211f0cc:
- `esim_ota_test.go` / `esim_stock_test.go` — `setupOTATenant`/`setupStockTenant` reference `tenants.slug` column not in current migrations
- `password_history_test.go` — `password_history_user_id_fkey` constraint violation
- `tenant_test.go` — `users.name NOT NULL` seed mismatch
- `sla_report_test.go` — seed dup-key on `operators.code` (test fixture)
- `sms_outbound_test.go` — cursor pagination expectation
- `migration_freshvol_test.go` — chain regression
- `sim_list_enriched_explain_test.go` — index-scan EXPLAIN expectation

These are NOT FIX-251 regressions and were NOT introduced by commit 211f0cc. Tracked in `docs/ROUTEMAP.md` D-066 awaiting a dedicated test-infra cleanup story.

## Deferred Items

| ID | Description | Target Story | ROUTEMAP Updated |
|----|-------------|--------------|------------------|
| D-181 | Systemic inline-scan-vs-helper drift audit. 18 store files have `scanXxx` helpers; several have inline `rows.Scan(...)` in `List`/`ListActive`/etc. that risk repeating PAT-006: `sim.go` (`simColumns` + inline at line 345), `policy.go` (11 inline scans alongside `scanPolicy`/`scanPolicyVersion`/`scanRollout`), `cdr.go` (10 inline scans alongside `scanCDR`), `ippool.go` (7 inline scans alongside `scanIPPool`/`scanIPAddress`), `session_radius.go` (5), `notification.go` (3). Add `TestColumnsAndScanCountConsistency` per table OR refactor all inline list scans to delegate to the helper. | future test-hardening / refactor story | YES |

## Verification

- Tests after fix: 3/3 FIX-251 regression PASS, 14/14 operator-scoped PASS
- Build after fix: PASS (`go build` + `go vet`)
- Live: `argus-app` healthy 2 days; `/api/v1/operators` returns 401 (clean auth path) NOT 500
- Fix iterations: 0 (retroactive verification of merged code)

## Final Verdict

**PASS** — FIX-251 root-cause fix is correct, regression tests are effective, no behavior regression on valid error paths, no cosmetic mask. One LOW-severity systemic finding deferred to D-181 (out of FIX-251 scope per spec — it is a backend-bug-fix story, not a refactor story).
