# Post-Story Review: STORY-095 — IMEI Pool Management

> Date: 2026-05-03

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| STORY-096 | `device.imei_in_pool(...)` now functional; blacklist hard-deny ready to consume. D-187 (`simAllowlistStore` dormant) re-targeted here — wire or delete. D-189 (`bound_sims_count=0`) deferred decision. | UPDATED |
| STORY-097 | D-188 (API-335 `bound_sims` + `history` empty arrays) targeted here. SCR-197 drawer pre-wired with empty states. `IMEIPoolStore.LookupKind` functional for cross-reference. | UPDATED |
| STORY-098 | No dependency on IMEI pool infrastructure. | NO_CHANGE |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/USERTEST.md` | Appended `## STORY-095:` section (17 scenarios UT-095-01..17) | UPDATED |
| `docs/GLOSSARY.md` | Added 9 terms: IMEI Pool, Whitelist (IMEI), Greylist (IMEI), Blacklist (IMEI), Operator EIR, Pool Lookup, Move-Between-Lists, CSV Injection Guard + TAC Range already existed | UPDATED |
| `docs/brainstorming/decisions.md` | Added VAL-052 (Move-Between-Lists DELETE+POST contract), VAL-053 (bulk-select Promise.allSettled), VAL-054 (CSV_INJECTION_REJECTED scope). VAL-048..051 confirmed pre-existing. | UPDATED |
| `docs/brainstorming/bug-patterns.md` | PAT-026 RECURRENCE [STORY-095 Gate F-A1] confirmed at line 44. No new pattern needed — this is a variant of PAT-026, not a new root class. | CONFIRMED |
| `docs/architecture/api/_index.md` | Fixed stale bold total from 269→276 (API-331..335 rows already present from Phase 11 architect dispatch). Added changelog entry. | UPDATED |
| `docs/architecture/db/_index.md` | TBL-56/57/58 confirmed present. No change needed. | CONFIRMED |
| `docs/architecture/ERROR_CODES.md` | Added 9 new codes: INVALID_POOL_KIND, INVALID_ENTRY_KIND, INVALID_TAC, MISSING_QUARANTINE_REASON, MISSING_BLOCK_REASON, INVALID_IMPORTED_FROM, IMEI_POOL_DUPLICATE, POOL_ENTRY_NOT_FOUND, CSV_INJECTION_REJECTED (+ Go constants). | UPDATED |
| `docs/architecture/DSL_GRAMMAR.md` | Updated `device.imei_in_pool` row to note functional status, TAC-range matching behavior, per-pass cache, and "Functional as of STORY-095" annotation. | UPDATED |
| `docs/SCREENS.md` | SCR-196 + SCR-197 confirmed present. No change needed. | CONFIRMED |
| `docs/FRONTEND.md` | No new design tokens or patterns introduced. | NO_CHANGE |
| `docs/ROUTEMAP.md` | (Updated last — see below) | UPDATED |
| `docs/stories/phase-11/STORY-096-binding-enforcement.md` | Prepended STORY-095 Handoff Notes: imei_in_pool functional, D-187/D-189 disposition, blacklist hard-deny, PAT-026 guard. | UPDATED |
| `docs/stories/phase-11/STORY-097-imei-change-detection.md` | Prepended STORY-095 Handoff Notes: D-188 target, SCR-197 drawer pre-wired, SIM cross-link nav. | UPDATED |
| `docs/architecture/CONFIG.md` | No new env vars introduced by STORY-095. | NO_CHANGE |
| `cmd/argus/main.go` | F-A1: BulkIMEIPoolImportProcessor instantiation + SetAuditor + Register confirmed at lines 849-881. | CONFIRMED (code) |
| `internal/api/imei_pool/handler.go` | F-A5: `hasCSVInjectionPrefix` present (3 hits). | CONFIRMED (code) |
| `internal/policy/enforcer/enforcer.go` | F-A8: `sessionCtx.WithContext(ctx)` at line 186. | CONFIRMED (code) |
| `internal/policy/dsl/evaluator.go` | T6: `LookupKind` called at line 439; per-pass cache wired at lines 50, 103. | CONFIRMED (code) |

## Cross-Doc Consistency

- Contradictions found: 0
- The ERROR_CODES.md was missing 9 IMEI pool codes that existed in `internal/apierr/apierr.go` — gap closed.
- api/_index.md bold total line was stale (269 vs actual 276 per changelog) — corrected.
- DSL_GRAMMAR.md row 152 implied functional status but lacked explicit annotation — clarified.
- db/_index.md TBL-56/57/58 confirmed registered; no stale total line to update.
- SCREENS.md SCR-196/197 confirmed at lines 94-95; total screen count unchanged.

## Decision Tracing

- VAL-048..051 in decisions.md: all 4 confirmed present, correctly tagged ACCEPTED.
- New decisions added: VAL-052 (Move action contract), VAL-053 (Promise.allSettled), VAL-054 (error code scope). 3 implicit implementation decisions were unrecorded — now captured.
- Orphaned decisions (approved but not applied): 0.

## USERTEST Completeness

- Entry exists: YES (appended in this review)
- Type: UI scenarios (17 scenarios: 8 backend + 9 UI/frontend)
- Covers: API-331..335, CSV injection (Gate F-A5), bulk import (Gate F-A1), Move action (Gate F-A2), Bulk-select (Gate F-A3), CSV pre-flight (Gate F-A11), SCR-196 4 tabs, SCR-197 3 entry points + drawer.

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting this story: 0 pre-existing tech debt items targeted STORY-095.
- D-188 and D-189 were created BY this story's gate; both confirmed written to ROUTEMAP.
- D-187 (STORY-094 carry-over) confirmed re-targeted from STORY-095 to STORY-096.

## Mock Status

- Not applicable — Argus does not use `src/mocks/` directory; all API integration uses real endpoints.

## Check Results (14/14)

| # | Check | Status | Notes |
|---|-------|--------|-------|
| 1 | Doc consistency — Story ACs vs implementation | PASS | 14/14 ACs PASS per Gate; VAL-048..050 spec amends applied |
| 2 | decisions.md VAL entries | PASS | VAL-048..051 confirmed; VAL-052..054 added |
| 3 | bug-patterns.md PAT-026 RECURRENCE | PASS | Line 44 confirmed; no new pattern needed (same root class as PAT-026) |
| 4 | USERTEST.md STORY-095 section | PASS | 17 scenarios appended (UT-095-01..17) |
| 5 | GLOSSARY.md new terms | PASS | 9 terms added; GSMA CEIR + TAC Range pre-existing |
| 6 | api/_index.md — 5 endpoints registered | PASS | API-331..335 at lines 598-602; total corrected 269→276 |
| 7 | db/_index.md — 3 tables registered | PASS | TBL-56/57/58 at lines 65-67 |
| 8 | CONFIG.md — no new env vars | PASS | No IMEI pool env vars in CONFIG.md or code; confirmed |
| 9 | ERROR_CODES.md — 9 codes added | PASS | 9 codes + Go constants added |
| 10 | DSL_GRAMMAR.md — imei_in_pool functional | PASS | Row 152 updated with functional annotation + TAC-range + cache notes |
| 11 | FRONTEND.md / SCREENS.md | PASS | SCR-196/197 confirmed; no new design tokens |
| 12 | Story Impact — downstream story updates | PASS | STORY-096 + STORY-097 handoff notes prepended; STORY-098 NO_CHANGE |
| 13 | Findings Resolution — 0 ESCALATED/OPEN | PASS | See Findings Resolution table below |
| 14 | Final build/test verification | PASS | All 5 gates green (see below) |

## Build/Test Verification

| Command | Result |
|---------|--------|
| `go build ./...` | PASS (exit 0) |
| `go vet ./...` | PASS (exit 0, clean) |
| `cd web && npx tsc --noEmit` | PASS (exit 0) |
| `cd web && npm run build` | PASS (4.09s) |
| `go test -count=1 ./...` | PASS — 3935 tests / 0 failures / 110 packages |

## Findings Resolution Table

| # | Finding | Gate Sev | Gate Disposition | Reviewer Verification | Final Status |
|---|---------|----------|------------------|-----------------------|--------------|
| F-A1 | BulkIMEIPoolImportProcessor not registered in main.go | CRITICAL | FIXED | `grep 'BulkIMEIPool' cmd/argus/main.go` → 4 hits (lines 849-881) | FIXED |
| F-A2 | Move-between-lists action missing | HIGH | FIXED | `pool-list-tab.tsx` Move action confirmed in Gate report; FE builds clean | FIXED |
| F-A3 | Bulk-select toolbar missing | HIGH | FIXED | Checkbox + selection toolbar in `pool-list-tab.tsx`; FE builds clean | FIXED |
| F-A4 | AC-11 8-digit TAC vs 15-digit IMEI reconcile | HIGH | VAL-048 | VAL-048 in decisions.md ✓ | VAL |
| F-A5 | CSV-injection guard missing on Add handler | HIGH | FIXED | `hasCSVInjectionPrefix` 3 hits in handler.go | FIXED |
| F-A6 | API-335 bound_sims + history empty | MEDIUM | DEFERRED → D-188 | D-188 in ROUTEMAP targeting STORY-097 ✓ | DEFERRED |
| F-A7 | API-331 bound_sims_count=0 placeholder | MEDIUM | DEFERRED → D-189 | D-189 in ROUTEMAP targeting STORY-096 ✓ | DEFERRED |
| F-A8 | Enforcer ctx not propagated to LookupKind | MEDIUM | FIXED | `sessionCtx.WithContext(ctx)` at enforcer.go:186 | FIXED |
| F-A9 | FE test pattern (vitest absent) | MEDIUM | NOT_APPLICABLE | tsc-throw smoke pattern confirmed; no vitest hooks | N/A |
| F-A10 | AC-12 INSUFFICIENT_PERMISSIONS vs INSUFFICIENT_ROLE | LOW | VAL-049 | VAL-049 in decisions.md ✓ | VAL |
| F-A11 | BulkImportTab CSV pre-flight not wired | LOW | FIXED | `hasCSVInjection()` in bulk-import-tab.tsx; FE builds clean | FIXED |
| F-U1 | Drawer animation 300ms vs 280ms spec | LOW | VAL-050 | VAL-050 in decisions.md ✓ | VAL |
| F-U2 | Dynamic-width inline style on progress bar | LOW | NOT_APPLICABLE | Allowed pattern per project convention | N/A |

**Zero ESCALATED / OPEN / NEEDS_ATTENTION findings.**

## Project Health

- Stories completed: 3/6 Phase 11 (50%) — STORY-092, STORY-094, STORY-095 DONE
- Current phase: Phase 11 — Enterprise Readiness Pack
- Next story: STORY-096 — Binding Enforcement & Mismatch Handling (P0)
- Blockers: None. STORY-095 IMEI pool tables + DSL predicate unblock STORY-096 blacklist hard-deny.
