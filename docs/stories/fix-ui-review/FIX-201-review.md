# Post-Story Review: FIX-201 — Bulk Actions Contract Fix

> Date: 2026-04-20

## Impact on Upcoming Stories

| Story | Impact | Action |
|-------|--------|--------|
| FIX-202 | SIM list DTO enrichment (JOIN adds `operator_name`); FIX-201 added per-row spinner overlay. No structural collision — spinner is a presentational state overlay keyed by `processingIds`, orthogonal to DTO field additions. | NO_CHANGE |
| FIX-206 | Orphan cleanup + FK constraints. FIX-201's `FilterSIMIDsByTenant` correctly returns foreign/missing SIM IDs as violations regardless of orphan state (no change in behavior when FIX-206 adds FK constraints). | NO_CHANGE |
| FIX-208 | Cross-tab aggregation. FIX-201 changed no SIM DTO shape, only added `sim_ids` input path and job response `{job_id, total_sims, status}`. No contract drift affecting aggregation. | NO_CHANGE |
| FIX-216 | Modal pattern standardization. FIX-201 Gate Follow-Up Flag: `useBulkPolicyAssign` hook does not send `reason` in request body → audit entries emit `reason=""` for bulk policy-assign until this hook is patched. FIX-216 is the logical home for this fix. Tracked as D-044. | UPDATED (see D-044) |

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/stories/fix-ui-review/FIX-201-review.md` | This report (new) | UPDATED |
| `docs/USERTEST.md` | FIX-201 section appended (4 manual test scenarios) | UPDATED |
| `docs/brainstorming/decisions.md` | Added DEV-256 (shipping decision) | UPDATED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-006 (shared payload struct field propagation) | UPDATED |
| `docs/ROUTEMAP.md` | FIX-201 status → `[x] DONE`; added D-044, D-045 tech debt rows | UPDATED |
| `docs/architecture/MIDDLEWARE.md` | No changes needed (F-A3 gate fix already correct) | NO_CHANGE |
| `docs/architecture/api/bulk-actions.md` | No changes needed (gate already corrected wording) | NO_CHANGE |
| `docs/architecture/api/_index.md` | No changes needed (gate rows already updated) | NO_CHANGE |
| `docs/architecture/ERROR_CODES.md` | No changes needed — `FORBIDDEN_CROSS_TENANT` and `SERVICE_DEGRADED` documented correctly | NO_CHANGE |
| `docs/ARCHITECTURE.md` | No architectural changes introduced by this story | NO_CHANGE |
| `docs/SCREENS.md` | SCR-080 already covers sticky bulk bar; no new screen | NO_CHANGE |
| `docs/FRONTEND.md` | No contradictions with design tokens used in `sims/index.tsx` | NO_CHANGE |
| `docs/PRODUCT.md` | No contradiction with bulk operation business rules | NO_CHANGE |
| `docs/FUTURE.md` | No new future opportunities or invalidations | NO_CHANGE |
| `Makefile` | No new services, scripts, or targets | NO_CHANGE |
| `CLAUDE.md` | No Docker URL/port changes | NO_CHANGE |

## Cross-Doc Consistency

Contradictions found: 1 (resolved via DEFERRED)

**Discrepancy:** `ERROR_CODES.md` documents `SERVICE_DEGRADED` and its Go constant name as `CodeServiceDegraded`, and the code block in that doc lists it. However, `internal/apierr/apierr.go` does NOT define a `CodeServiceDegraded` constant — `bulk_handler.go:90` uses the raw string literal `"SERVICE_DEGRADED"` directly. This pre-dates FIX-201 (kill-switch path was not modified by this story), but FIX-201 explicitly added `SERVICE_DEGRADED` to the `bulk-actions.md` error catalog while leaving the underlying Go-constant gap unfixed. Tracked as D-045.

## Decision Tracing

- Decisions checked: 1 (DEV-256 added for FIX-201 shipping)
- Orphaned (approved but not applied): 0
- The plan's rate-limit mechanism decision (intentionally NOT via `LimitKey` — bulk rate-throttle is a separate concern from resource-cap) is captured in DEV-256.

## USERTEST Completeness

- Entry exists: NO (before this review)
- Type: MISSING → FIXED (4 UI scenarios appended to `docs/USERTEST.md`)

## Tech Debt Pickup (from ROUTEMAP)

- Items targeting FIX-201: 0 (D-001..D-040 all pre-date this story; none targeted FIX-201)
- Already `✓ RESOLVED` by Gate: 0 (no pre-existing items targeted FIX-201)
- Resolved by Reviewer (Gate missed marking): 0
- NOT addressed (CRITICAL): 0

New items created by this review:
- D-044 → FIX-216 (FE PolicyAssign+OperatorSwitch hooks do not send `reason`)
- D-045 → future error-code hygiene story (`SERVICE_DEGRADED` missing from `apierr.go` constants)

## Mock Status

Not applicable — no `src/mocks/` directory. FE calls real backend directly.

## Issues

| # | Issue | Severity | Resolution | Detail |
|---|-------|----------|------------|--------|
| 1 | `useBulkPolicyAssign` FE hook (and absent `useBulkOperatorSwitch` hook) does not include `reason` field in request body. Backend `BulkPolicyAssignPayload.Reason` propagation fix (F-A1 gate) will surface `reason=""` in audit entries for all bulk policy-assign operations until hook is patched. | NON-BLOCKING | DEFERRED D-044 → FIX-216 | Gate documented this as "Follow-Up Flag". FIX-216 (Modal Pattern Standardization) touches the same FE hooks and is the logical home. Audit chain integrity not broken (`reason` is optional per domain semantics); compliance impact is cosmetic absence of reason string in audit entries. |
| 2 | `internal/api/sim/bulk_handler.go:90` uses raw string `"SERVICE_DEGRADED"` instead of a named constant. `ERROR_CODES.md` code block lists `CodeServiceDegraded` as the Go constant, but `internal/apierr/apierr.go` defines no such constant. Cross-doc contradiction introduced by FIX-201's Task 10 (which added `SERVICE_DEGRADED` to `bulk-actions.md` error catalog) without patching the underlying Go constant gap. | NON-BLOCKING | DEFERRED D-045 → future error-code hygiene story | Pre-existing issue (kill-switch branch predates FIX-201). No runtime regression — the raw string produces correct HTTP responses. Consistency fix should sweep all raw-string error code usages across handlers in a single story. |

## Project Health

- Stories completed: FIX-201/30 UI-review stories (1 done, 29 remaining)
- Current phase: UI Review Remediation — Wave 1
- Next story: FIX-202 (SIM List & Dashboard DTO — Operator Name Resolution)
- Blockers: None
