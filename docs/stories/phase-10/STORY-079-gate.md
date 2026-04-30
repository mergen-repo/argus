# Gate Report: STORY-079

## Summary
- Requirements Tracing: Fields 5/5, Endpoints 1/1 (status/details), Workflows 9/9 ACs addressed, Components 4/4 (compare.tsx, sims/index.tsx, router.tsx, api.ts)
- Gap Analysis: 9/9 acceptance criteria passed (AC-1..AC-9 all DONE; AC-7 widened from 60s → 300s window during Gate)
- Compliance: COMPLIANT
- Tests: 71/71 story-package tests passed (up from 69 — added 2 coverage cases), 2870/2870 full suite passed (up from 2868)
- Test Coverage: 9/9 ACs with verification, 1 AC (AC-7) hardened with two new negative tests (window-expiry at 299s/300s/900s + 5-minute spread)
- Performance: 1 observed (errorRingBuffer mutex hot path) — deferred per plan acceptance
- Build: PASS (go build, tsc --noEmit, vite production build)
- Screen Mockup Compliance: all 6 scout scenarios matched (compare empty/hydrated/invalid, /dashboard, AC-6 no-toast, sessions regression)
- UI Quality: 14/14 criteria PASS, 0 NEEDS_FIX, 0 CRITICAL
- Token Enforcement: 0 violations found (hex / px / raw HTML / library / defaults / inline SVG / shadow-none — all clear)
- Turkish Text: n/a (AC-8 is decision-only; posture recorded)
- Overall: PASS

## Team Composition
- Analysis Scout: 8 findings (F-A1..F-A8)
- Test/Build Scout: 0 findings (baseline green)
- UI Scout: 3 findings (F-U1..F-U3; F-U3 informational only)
- De-duplicated: 11 → 11 findings (no cross-scout overlaps — each scout touched different subsystems)

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | Gap (HIGH) | `internal/observability/metrics/metrics.go` | Widened `errorRingBuffer` from 60 → 300 one-second slots so `recent_error_5m` actually reports a 5-minute window (was 60s). Introduced `recent5xxWindowSeconds = 300` constant; updated `record()` / `sum()` loops; updated `Recent5xxCount` docstring to state 300s / 5 min. | Tests pass |
| 2 | Gap (HIGH) | `internal/observability/metrics/metrics_test.go` | Renamed expiry subtest label `60s → 300s`; pegged boundary checks at +299s / +300s / +900s; added new subtest "records spread across 5 minutes all counted" (60 hits @ 5s cadence, all in-window at +295s, all dropped at +600s). | 71/71 pass |
| 3 | Compliance (LOW) | `cmd/argus/main_cli_test.go` | Renamed mislabeled subtest `"serve subcommand"` → `"migrate subcommand (no direction)"` (it was passing `["migrate"]`, not serve). Added genuine positive serve-subcommand case `{args: ["serve"], wantSub: "serve"}`. | Pass |
| 4 | Gap (LOW) | `cmd/argus/main.go` | Replaced 4 occurrences of `err == migrate.ErrNoChange` with `errors.Is(err, migrate.ErrNoChange)` (Go idiom; robust to wrapping). Added `"errors"` import. | Build + tests pass |
| 5 | UI (LOW) | `web/src/lib/api.ts` | Added `UUID_RE` client-side guard in `authApi.revokeSession`: rejects empty/malformed IDs before dispatching a request the server would just return INVALID_FORMAT for. Belt-and-suspenders with the existing response-interceptor silence. | tsc + vite build pass |

## Escalated Issues (architectural / business decisions)

None. All HIGH-severity findings were in-scope and fixable.

## Deferred Items (tracked in ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Written to ROUTEMAP |
|---|---------|-------------|---------------------|
| D-1 | F-U1: /dashboard alias does not trigger "Dashboard" active state in sidebar (cosmetic — picked `element: <DashboardPage />` reuse, sidebar `href="/"` doesn't match `/dashboard`). Fix: add `/dashboard` to sidebar active matcher or switch alias to `<Navigate to="/" replace />`. | POST-GA UX polish | YES |
| D-2 | F-A3: Decisions numbering drift — plan predicted DEV-233/234 for AC-8/9; impl used 231/232/233 for planner + 234/235 for AC-8/9. Decisions immutable once written; cosmetic only. | N/A (cosmetic, no remediation) | YES (documented but no target action) |
| D-3 | F-A4: No CI guard against future CHECK-constraint / seed drift. Today's AC-3 fix patches 6 enum values but no automated fresh-volume test. | POST-GA CI hardening | YES |
| D-4 | F-A6: `argus seed <file>` accepts arbitrary absolute paths / `..` escapes `seedPath`. Low-priority hardening — operator-invoked only, DB role already privileged. | POST-GA security hardening | YES |
| D-5 | F-A7: `errorRingBuffer` takes `sync.Mutex` on every recorded 5xx. Fine today (only hot under error conditions), single hotspot at >10k RPS. Plan explicitly accepted the mutex. | POST-GA perf | YES |
| D-6 | F-A8: ROUTEMAP D-013..D-021 Phase 10 post-gate-sweep entries flip to RESOLVED — Ana Amil close-out task (not Gate scope). | STORY-079 close commit | Ana Amil handles |

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `internal/observability/metrics/metrics.go:19-59` | errorRingBuffer sum over 60→300 slots | Mutex per record; O(300) scan on each /status/details call | LOW | DEFERRED (D-5) — scan still trivial (<5μs typical) |

### Caching Verdicts
None new this story. `/api/v1/status/details` is per-request gathered; caching not applicable.

## Token & Component Enforcement (UI stories)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAR |
| Arbitrary pixel values | 0 | 0 | CLEAR |
| Raw HTML elements | 0 | 0 | CLEAR |
| Competing UI library imports | 0 | 0 | CLEAR |
| Default Tailwind colors | 0 | 0 | CLEAR |
| Inline SVG | 0 | 0 | CLEAR |
| Missing elevation | 0 | 0 | CLEAR |

(Scout UI ran 7 enforcement greps; all 0 matches before Gate. No UI files touched by Gate fixes except `web/src/lib/api.ts` which is not a component file and is not subject to token enforcement.)

## Verification
- Go build after fixes: PASS
- Go tests after fixes: 2870/2870 passed (94 pkgs), up from 2868 baseline (+2 new coverage cases)
- Touched-package tests: 71/71 passed (`internal/observability/metrics` + `internal/api/system` + `cmd/argus`)
- TypeScript typecheck (`npx tsc --noEmit`): PASS
- Token enforcement: ALL CLEAR (0 violations)
- Fix iterations: 1 (no re-check needed — everything green on first verify)

## Maintenance Mode — Pass 0 Regression

Not applicable. STORY-079 is a development-phase audit sweep (maintenance_mode: NO).

## Passed Items

- **AC-1 (`argus migrate`/`seed`/`version` CLI)**: wired; `parseSubcommand` dispatches before `config.Load`; `runMigrate` + `runSeed` implemented with golang-migrate reuse. `make db-migrate` works.
- **AC-2 (CONCURRENTLY demotion)**: 8 offending migrations demoted (parent `CREATE INDEX` without CONCURRENTLY, per-partition CONCURRENTLY retained where safe). Story's "6 files" figure updated to 8 matching diff.
- **AC-3 (Seed fresh-volume fix)**: 6 enum CHECK-constraint mismatches patched; seed header documents root cause as CHECK constraints (not RLS as originally hypothesized).
- **AC-4 (`/sims/compare` auto-populate + Compare button)**: `useSearchParams` mounted; sim_id_a/sim_id_b hydrate inputs; Compare button on `/sims` list navigates with the pair.
- **AC-5 (`/dashboard` alias)**: route registered via element reuse (Option A). Deferred D-1 tracks the sidebar active-state cosmetic gap.
- **AC-6 (Silence Invalid-session toast)**: response interceptor silences `/auth/sessions/<id>` 400 INVALID_FORMAT (forensic breadcrumb preserved via server log `session_id`). Gate added UUID_RE guard at the call site as extra belt-and-suspenders (F-U2 fix).
- **AC-7 (Live `recent_error_5m`)**: ring-buffer live; **Gate fix widened window 60→300s** so the field name matches its semantics (F-A1 fix). Two new negative tests verify boundary + spread behavior.
- **AC-8 (Turkish i18n posture)**: decision recorded (DEV-234: DEFER to dedicated localization story post-GA).
- **AC-9 (`/policies` Compare posture)**: decision recorded (DEV-235: NO — close the Phase 10 gate note recommendation).

## Evidence References

- Scout UI screenshots: `.playwright-mcp/story079-{dashboard-post-login,dashboard-alias,compare-empty,compare-hydrated,compare-invalid-uuid,settings-sessions}.png`
- Phase 10 gate origin: `docs/reports/phase-10-gate.md` (F-1..F-8)
- STORY-067 DEV-191 origin: reviewed & resolved via AC-7
- Audit report: `docs/reports/compliance-audit-report.md` (2026-04-15)
