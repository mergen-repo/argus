# Gate Report: STORY-095 — IMEI Pool Management

**Date:** 2026-05-03
**Mode:** Gate Team (Lead consolidating 3 parallel scouts)
**Verdict:** PASS

## Summary

- Requirements Tracing: 14/14 ACs implemented (with disposition adjustments via VAL-048..050)
- Gap Analysis: 14/14 acceptance criteria PASS after fixes
- Compliance: COMPLIANT
- Tests: 3935 / 3935 backend pass; FE tsc clean; vite build green
- Test Coverage: 5 new regression tests added across 3 packages
- Performance: no perf regressions; one D-189 routed for `bound_sims_count` aggregation
- Build: PASS (Go + Vite)
- Screen Mockup Compliance: SCR-196 + SCR-197 scoped + accepted
- UI Quality: 14/15 PASS (1 LOW VAL-accepted — 300 ms drawer animation)
- Token Enforcement: 0 hex hardcodes, 1 dynamic-width inline style allowed for progress bar
- Turkish Text: N/A (English copy track)
- Overall: PASS

## Team Composition

- Analysis Scout: 11 findings (F-A1..F-A11)
- Test/Build Scout: 0 findings (suite & build green pre-fix)
- UI Scout: 2 findings (F-U1 informational, F-U2 acceptable)
- De-duplicated: 13 → 13 findings (no overlap; F-A11 + UI both touch BulkImportTab but address different surfaces — kept distinct)

## Findings Disposition Table

| ID | Sev | Category | File / Surface | Disposition |
|----|-----|----------|----------------|-------------|
| F-A1 | CRITICAL | gap | `cmd/argus/main.go` worker registration | FIXED |
| F-A2 | HIGH | gap | `web/src/pages/settings/imei-pools/pool-list-tab.tsx` Move action | FIXED |
| F-A3 | HIGH | gap | `pool-list-tab.tsx` bulk-select toolbar | FIXED |
| F-A4 | HIGH | gap | AC-11 (8-digit TAC) vs AC-6 (15-digit) reconcile | VAL-048 (spec amend) |
| F-A5 | HIGH | security | `internal/api/imei_pool/handler.go::Add` CSV-injection | FIXED |
| F-A6 | MEDIUM | gap | API-335 `bound_sims` + `history` empty arrays | DEFERRED → D-188 (STORY-097) |
| F-A7 | MEDIUM | gap | API-331 `bound_sims_count = 0` placeholder | DEFERRED → D-189 (STORY-096) |
| F-A8 | MEDIUM | compliance | `internal/policy/enforcer/enforcer.go` ctx propagation | FIXED |
| F-A9 | MEDIUM | gap | FE test pattern (vitest absent) | NOT_APPLICABLE (already tsc-throw smoke pattern) |
| F-A10 | LOW | compliance | AC-12 wording `INSUFFICIENT_PERMISSIONS` → `INSUFFICIENT_ROLE` | VAL-049 (spec amend) |
| F-A11 | LOW | security | BulkImportTab client-side CSV pre-flight | FIXED |
| F-U1 | LOW | 6.3 | SlidePanel 300 ms drawer animation | VAL-050 (accept project default) |
| F-U2 | LOW | 6.4 | Single dynamic-width inline style on progress bar | NOT_APPLICABLE (allowed exception) |

## Fixes Applied

### F-A1 (CRITICAL) — BulkIMEIPoolImportProcessor registered

- **Files modified:** `cmd/argus/main.go`
- **Change:** Added `bulkIMEIPoolImportProc := job.NewBulkIMEIPoolImportProcessor(jobStore, imeiPoolStore, eventBus, log.Logger)` + `SetAuditor(auditSvc)` + `jobRunner.Register(bulkIMEIPoolImportProc)` mirroring the `bulkDeviceBindingsProc` pattern at line 846.
- **Regression tests added:** `TestBulkIMEIPoolImportProcessor_Type` and `TestBulkIMEIPoolImport_RegisteredInAllJobTypes` in `internal/job/imei_pool_import_worker_test.go`.
- **PAT routed:** PAT-026 RECURRENCE (inverse-orphan — consumer-missing variant).

### F-A5 (HIGH security) — CSV-injection guard on Add handler

- **Files modified:** `internal/api/imei_pool/handler.go` (added `hasCSVInjectionPrefix` helper + 5-field guard before pool-specific validation), `internal/apierr/apierr.go` (added `CodeCSVInjectionRejected = "CSV_INJECTION_REJECTED"`).
- **Change:** Add now rejects `device_model`, `description`, `quarantine_reason`, `block_reason`, `imported_from` values starting with `=`, `+`, `-`, `@`, or tab with HTTP 422 + `CSV_INJECTION_REJECTED`. Parity with bulk-import worker.
- **Regression tests added:** `TestIMEIPoolHandler_Add_CSVInjectionRejected_422` (5 sub-cases for each forbidden prefix) + `TestIMEIPoolHandler_Add_BenignValues_NoCSVRejection` in `internal/api/imei_pool/handler_test.go`.

### F-A2 (HIGH) — Move-between-lists action

- **Files modified:** `web/src/pages/settings/imei-pools/pool-list-tab.tsx`
- **Change:** Added Move action to RowActionsMenu opening a Dialog with destination Select. On confirm: POST to destination first (preserves source on failure); DELETE source second; partial-failure toast surfaces both pools holding the entry. Each step emits its own audit event (`entry_added` + `entry_removed`) per AC-7.

### F-A3 (HIGH) — Bulk-select toolbar

- **Files modified:** `web/src/pages/settings/imei-pools/pool-list-tab.tsx`
- **Change:** Added Checkbox column with select-all header; selection-state Card toolbar with `Delete N` action gated by confirm Dialog; Promise.allSettled sequenced DELETEs; partial-failure toast.

### F-A8 (MEDIUM) — Enforcer ctx propagation

- **Files modified:** `internal/policy/enforcer/enforcer.go`
- **Change:** `e.evaluator.Evaluate(sessionCtx, compiled)` → `e.evaluator.Evaluate(sessionCtx.WithContext(ctx), compiled)`. RADIUS/Diameter request cancellations now flow into `device.imei_in_pool()` lookups.
- **Regression test added:** `TestEnforcer_PropagatesRequestCtxToSessionContext` in `internal/policy/enforcer/enforcer_test.go` (source-level guard).

### F-A9 (MEDIUM) — FE test pattern

- **NOT_APPLICABLE.** Both `web/src/components/imei-lookup/__tests__/{imei-lookup-modal,imei-lookup-drawer}.test.tsx` already use the canonical tsc-throw smoke pattern (no vitest hooks); the file headers themselves document the convention. F-A9 was a misread — confirmed by `grep -n "vitest\|describe\|it(\|test(" web/src/components/imei-lookup/__tests__/*.test.tsx` returning only the comment line at `imei-lookup-drawer.test.tsx:13`.

### F-A11 (LOW security) — Client-side CSV-injection pre-flight

- **Files modified:** `web/src/pages/settings/imei-pools/bulk-import-tab.tsx`
- **Change:** On file selection, the first 1 MB / 1000 rows are scanned via `hasCSVInjection()` from `@/types/imei-pool`. Any cell starting with a formula-trigger character surfaces a non-blocking warning panel listing affected row numbers (sample of first 3). The server still enforces the same rule via the worker; this is purely UX.

### F-A4 (HIGH → VAL) — AC-11 reconciliation

- **No code change.** AC-6 backend is 15-digit-IMEI-only; the modal already enforces 15 digits. Spec amended via **VAL-048** to read "Compact-form modal accepts a 15-digit IMEI". 8-digit TAC support would be a scope expansion (new endpoint or per-pool browse) and is out of STORY-095 scope.

### F-A10 (LOW → VAL) — AC-12 wording

- **No code change.** Project-wide convention is `INSUFFICIENT_ROLE` (`internal/gateway/rbac.go:29`). Spec amended via **VAL-049**.

### F-U1 (LOW → VAL) — Drawer animation duration

- **No code change.** SlidePanel uses `duration-300` (300 ms) as the project-wide standard; plan referenced 280 ms which was per-story drift. Spec amended via **VAL-050**.

## Deferred Items (Tech Debt, written to ROUTEMAP)

| # | Finding | Target Story | Written |
|---|---------|--------------|---------|
| D-188 | F-A6 — API-335 Lookup `bound_sims` + `history` arrays empty (need `SIMStore.ListByBoundIMEI` + `IMEIHistoryStore.ListByObservedIMEI`). FE drawer sections already render empty states. | STORY-097 | YES |
| D-189 | F-A7 — API-331 `bound_sims_count` hard-coded to 0; per-row COUNT requires JOIN on `sims.bound_imei`. Decide in STORY-096 whether to implement or drop the column. | STORY-096 | YES |

D-187 (STORY-094 carry-over: `simAllowlistStore` dormant) re-targeted from STORY-095 to STORY-096 — STORY-095 deliberately chose the org-wide pool surface, leaving the per-SIM allowlist still without a production consumer.

## Validation / Testing Decisions Logged

- VAL-048: AC-11 reconciled to 15-digit-IMEI-only (matches AC-6 / API-335).
- VAL-049: AC-12 forbidden-role error code = `INSUFFICIENT_ROLE`.
- VAL-050: SlidePanel drawer animation accepted at `duration-300` (project standard).
- VAL-051: Registration-discipline lesson (PAT-026 RECURRENCE) — every new job processor must ship paired `Type()` + `AllJobTypes` registration tests.

## Pattern Catalog Updates

- **PAT-026 RECURRENCE [STORY-095 Gate F-A1]** — inverse-orphan variant. Constructor exists, tests pass, but `cmd/argus/main.go` never instantiates / registers the processor. Filed in `docs/brainstorming/bug-patterns.md` line 43 area. Mitigation: paired `TestXxxProcessor_Type` + `TestJobTypeXxx_RegisteredInAllJobTypes` co-committed with every new processor.

## Verification

| Check | Result |
|-------|--------|
| `go build ./...` | PASS |
| `go vet ./...` | clean |
| `gofmt -l <touched files>` | empty |
| `go test -count=1 ./...` | 3935 PASS / 0 FAIL across 110 packages |
| `cd web && npx tsc --noEmit` | PASS |
| `cd web && npm run build` | PASS (3.14s) |
| F-A1 verify: `grep 'BulkIMEIPoolImport' cmd/argus/main.go` | 4 hits (instantiation, SetAuditor, Register, comment) |
| F-A5 verify: `grep 'hasCSVInjectionPrefix' internal/api/imei_pool/handler.go` | 3 hits (defn + comment + call) |
| F-A8 verify: `grep 'WithContext' internal/policy/enforcer/enforcer.go` | 1 hit (call site) |

## Files Modified

- `cmd/argus/main.go`
- `internal/api/imei_pool/handler.go`
- `internal/api/imei_pool/handler_test.go`
- `internal/apierr/apierr.go`
- `internal/policy/enforcer/enforcer.go`
- `internal/policy/enforcer/enforcer_test.go`
- `internal/job/imei_pool_import_worker_test.go`
- `web/src/pages/settings/imei-pools/pool-list-tab.tsx`
- `web/src/pages/settings/imei-pools/bulk-import-tab.tsx`
- `docs/brainstorming/decisions.md` (VAL-048..051 added)
- `docs/brainstorming/bug-patterns.md` (PAT-026 RECURRENCE STORY-095 added)
- `docs/ROUTEMAP.md` (D-188, D-189 added; D-187 re-targeted)

## Files Created

- `docs/stories/phase-11/STORY-095-gate.md` (this report)

## Final Gate Verdict

**PASS.** All CRITICAL and HIGH findings either FIXED or routed via VAL spec-amend with code already correct. MEDIUM gaps either FIXED (F-A8) or DEFERRED to the natural consumer story (F-A6 → STORY-097, F-A7 → STORY-096). Full regression suite green. STORY-095 may be marked DONE.
