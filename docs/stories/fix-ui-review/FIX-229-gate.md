# Gate Report: FIX-229 — Alert Feature Enhancements

## Summary
- Requirements Tracing: AC 5/5 traced (1 plan-amendment for Comments-tab — see F-A2)
- Gap Analysis: 5/5 ACs PASS
- Compliance: COMPLIANT (PAT-016 zero, PAT-018 zero, FIX-216 modal pattern PASS, raw `<input type="radio">` outside `components/ui/` zero)
- Tests: 3594 PASS / 0 FAIL across 109 packages (28 new tests added vs. pre-Gate baseline of 3566)
- Performance: 1 N+1 fixed (~5050 → 2 queries on 10K-row PDF export)
- Build: PASS (go build, go vet, npm build 2.75s, tsc --noEmit clean)
- Token Enforcement: 0 violations (PAT-018 grep clean on 7 changed files)
- Overall: **PASS**

## Team Composition
- Analysis Scout: 10 findings (F-A1..F-A10)
- Test/Build Scout: 6 findings (F-B1..F-B6)
- UI Scout: 4 findings (F-U1..F-U4)
- De-duplicated: 20 → 16 unique (F-U1 = F-A2; F-U2 = F-A1)

## Triage Outcome
| ID | Disposition | Notes |
|----|-------------|-------|
| F-A1 / F-U2 | FIXED | dedup_key URL deeplink (BE filter + FE state hydration + viewAllHref) |
| F-A2 / F-U1 | DOCUMENTED | Plan amended — Comments-as-tab consciously dropped; existing Comments icon button preserved |
| F-A3 | FIXED | New `Radio` atom; 2 raw `<input type="radio">` sites swapped |
| F-A4 | FIXED | New `apierr.CodeDuplicate = "DUPLICATE"`; CreateSuppression + 2 tests routed to it |
| F-A5 | FIXED | `OperatorStore.ListNamesByIDs` + `SIMStore.ListICCIDsByIDs` (single ANY($1) query each); store_provider migrated |
| F-A6 | DEFERRED | D-NNN — export pagination 100-row chunk → FIX-248 reports engine migration |
| F-A7 | INFO | No fix needed (idx_alert_suppressions_active acceptable) |
| F-A8 | DEFERRED | D-NNN — "+N more" exact count (needs API contract change) → FIX-24x similar-alerts UX polish |
| F-A9 | FIXED | `DropdownMenuTrigger` now supports `asChild`; ExportMenu + alerts-page Mute trigger wrap `<Button>` atom |
| F-A10 | FIXED | `useAlertExport` routed through central `api` axios client (responseType: 'blob'); `/alerts/export` added to silentPaths to prevent double-toast; blob-error JSON re-parse preserved |
| F-B1 | FIXED | 3 new MatchActive scope tests (this/operator/dedup_key) |
| F-B2 | FIXED | Comment added on `TestUpsertWithDedup_AppliesActiveSuppression` referencing MatchActive scope coverage |
| F-B3..F-B6 | INFO | Pass confirmations only |
| F-U3 | FIXED | RetentionSection differentiates empty (Required) vs out-of-range (Must be between 30 and 365) |
| F-U4 | FIXED | Two `// V1 Tech Debt:` comments removed from `alert-rules.tsx` head |

## Fixes Applied

| # | Category | File:Line | Change | Verified |
|---|----------|-----------|--------|----------|
| 1 | spec/code consistency | `internal/apierr/apierr.go:41` | Added `CodeDuplicate = "DUPLICATE"` constant with comment | PASS (build/vet/test) |
| 2 | spec/code consistency | `internal/api/alert/handler.go:955` | CreateSuppression returns `CodeDuplicate` on duplicate rule_name | PASS |
| 3 | spec/code consistency | `internal/api/alert/handler_test.go:1509,1750` | Two assertions now expect `CodeDuplicate` | PASS |
| 4 | performance (N+1) | `internal/store/operator.go:256-281` | New `ListNamesByIDs(ctx, ids) (map[uuid.UUID]string, error)` | PASS |
| 5 | performance (N+1) | `internal/store/sim.go:214-239` | New `ListICCIDsByIDs(ctx, ids) (map[uuid.UUID]string, error)` | PASS |
| 6 | performance (N+1) | `internal/report/store_provider.go:389-413` | Provider hydrates operator names + sim ICCIDs via single ANY($1) per resource | PASS |
| 7 | test coverage | `internal/store/alert_suppression_test.go:283-405` | 3 new sibling tests for MatchActive `this`, `operator`, `dedup_key` scopes | PASS (DB-skip) |
| 8 | test coverage | `internal/store/alert_test.go:1057-1062` | Doc comment on `TestUpsertWithDedup_AppliesActiveSuppression` referencing MatchActive scope coverage | PASS |
| 9 | shadcn enforcement | `web/src/components/ui/radio.tsx` (NEW) | Radio atom mirroring Checkbox wrapper pattern | PASS |
| 10 | shadcn enforcement | `web/src/pages/alerts/_partials/mute-panel.tsx:267,317` | 2 raw radio inputs swapped for `<Radio>` atom; `sr-only` className passthrough preserved | PASS (build) |
| 11 | shadcn enforcement | `web/src/components/ui/dropdown-menu.tsx:46-83` | `DropdownMenuTrigger` now supports `asChild` via cloneElement | PASS |
| 12 | shadcn enforcement | `web/src/pages/alerts/_partials/export-menu.tsx` | Trigger wraps `<Button variant="outline" size="sm">` | PASS |
| 13 | shadcn enforcement | `web/src/pages/alerts/index.tsx:794-806` | Page Mute trigger wraps `<Button>` | PASS |
| 14 | security/consistency | `web/src/lib/api.ts:90-93` | `/alerts/export` added to silentPaths (prevents double-toast on blob 4xx) | PASS |
| 15 | security/consistency | `web/src/hooks/use-alert-export.ts` | Rewritten to use central `api` axios client (responseType: 'blob'); blob→JSON error re-parse via `Blob.text()` | PASS |
| 16 | gap (deeplink) | `internal/store/alert.go:96` | `ListAlertsParams.DedupKey string` field added | PASS |
| 17 | gap (deeplink) | `internal/store/alert.go:361-365` | ListByTenant filters by `dedup_key` when set | PASS |
| 18 | gap (deeplink) | `internal/api/alert/handler.go:228-230` | `parseAlertListFilters` reads `dedup_key` query param | PASS |
| 19 | gap (deeplink) | `web/src/pages/alerts/index.tsx:42-49,236-256,684-703,771` | `AlertFilters.dedup_key` field; `useSearchParams` hydration; query param forwarded; `hasFilters` includes it | PASS |
| 20 | gap (deeplink) | `web/src/pages/alerts/_partials/similar-alerts.tsx:58-77` | viewAllHref prefers `dedup_key` when anchor has one; falls back to `type+source` otherwise | PASS |
| 21 | UI bug | `web/src/pages/settings/alert-rules.tsx:268-336,361-374` | RetentionSection tracks raw input; empty → "Required", invalid range → existing error | PASS |
| 22 | code hygiene | `web/src/pages/settings/alert-rules.tsx:1-2` | 2 `// V1 Tech Debt:` head comments removed | PASS |
| 23 | plan amendment | `docs/stories/fix-ui-review/FIX-229-plan.md:333-353` | Mockup tab box updated to 2 tabs + amendment note documenting Comments-tab drop | PASS |

## Escalated Issues
None. All findings either fixed, deferred with target story, or informational.

## Deferred Items (to add to ROUTEMAP → Tech Debt)

> **Note for Ana Amil:** Add the two D-NNN entries below to `docs/ROUTEMAP.md → ## Tech Debt` (this gate does not modify ROUTEMAP per protocol).

| Proposed ID | Source | Description | Target Story |
|-------------|--------|-------------|--------------|
| D-NNN | FIX-229 Gate F-A6 | Alert export pagination uses 100-row sequential round-trips (10K rows = 100 RTTs). Server-side store cap blocks larger LIMIT. Migrate to single bounded LIMIT once reports engine streams in chunks. | FIX-248 (reports engine migration) |
| D-NNN | FIX-229 Gate F-A8 | Similar-alerts overflow hint shows "+ More similar exist" without an exact count. Requires `/alerts/{id}/similar` to return `meta.total` (currently caps at limit). | FIX-24x (similar-alerts UX polish) — pick a slot in remediation plan or carry to backlog |

## Performance Summary

### Queries Analyzed
| # | File:Line | Query/Pattern | Issue | Severity | Status |
|---|-----------|---------------|-------|----------|--------|
| 1 | `internal/report/store_provider.go:389-409` | per-id GetByID + GetICCIDByID in loops during PDF export | N+1 (~5050 RTTs at 10K rows / 50 ops / 5K SIMs) | MEDIUM | FIXED via batch ListNamesByIDs / ListICCIDsByIDs (2 queries) |
| 2 | `internal/api/alert/handler.go:272-294` | export pagination: 100-row chunk loop | sequential RTT-bound at large exports | LOW | DEFERRED → FIX-248 |

### Caching Verdicts
| # | Data | Location | TTL | Decision | Status |
|---|------|----------|-----|----------|--------|
| n/a | n/a | n/a | n/a | No new caching introduced or required | n/a |

## Token & Component Enforcement (UI scope)

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values | 0 | 0 | CLEAN |
| Raw HTML form elements outside `components/ui/` | 6 raw radios | 0 | FIXED (Radio atom) |
| Default Tailwind palette | 0 | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN |
| Raw button-shaped DropdownMenuTrigger | 2 sites | 0 | FIXED (asChild + Button atom) |
| FIX-216 modal pattern (SlidePanel + Dialog) | PASS | PASS | CLEAN |

## Verification

- `go build ./...` → PASS
- `go vet ./...` → PASS, no issues
- `go test ./...` → 3594 PASS / 0 FAIL across 109 packages (+28 vs baseline)
- `npm run build` → PASS, 2.75s
- `npx tsc --noEmit` → clean
- PAT-016 (analytics/anomalies path in alert-export) → 0 matches in 7 changed files
- PAT-018 (default Tailwind palette) → 0 matches in 7 changed files
- Raw radio grep outside `components/ui/` → 0 matches
- Fix iterations: 1 (no rework needed)

## Passed Items
- Suppression CRUD (CreateSuppression, ListSuppressions, DeleteSuppression) all return correct status codes; new DUPLICATE code is wire-aligned with plan §API Spec line 152.
- Per-tenant retention purge job (`AlertsRetentionProcessor`) preserves test invariants after spec/code consistency change.
- ListSimilar (anchor.dedup_key + type/source fallback) unchanged — already in green state pre-Gate.
- Mute panel SlidePanel UX (FIX-216 pattern) preserved after radio swap; `sr-only` className passthrough verified working through Radio wrapper.
- Export menu + Mute dropdown now inherit Button atom focus/hover/disabled styling; click handler routing through asChild verified by web build.
- Alerts page deeplink: `?dedup_key=<k>` and `?type=<t>&source=<s>` both parsed at mount, forwarded to `/alerts` list endpoint, and respected by `ListByTenant` SQL.

## GATE_RESULT: PASS
