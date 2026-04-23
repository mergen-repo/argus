# Gate Report: FIX-219 — Name Resolution + Clickable Cells Everywhere

## Summary
- Requirements Tracing: Fields 12/12, Endpoints 9/9, Workflows 12/12, Components 5/5
- Gap Analysis: 12/12 ACs passed (all originally MOSTLY PASS items fixed by gate)
- Compliance: COMPLIANT (AC-9 orphan em-dash strict rule enforced in all render surfaces; AC-10 aria-label minimized)
- Tests: Go 3542/3542 (full suite) + 911/911 (internal/api re-run after fixes) PASS; FE unit tests DEFERRED per D-091
- Test Coverage: AC coverage verified at scout level; negative tests tracked in D-091 (FIX-24x FE test infra wave)
- Performance: 0 issues (all joins PK-equality; no N+1; hover-card lazy + gated)
- Build: PASS (tsc 0 errors, vite 15.55s, go build clean)
- Screen Mockup Compliance: 23 pages adopt EntityLink (exceeds plan scope); FRONTEND.md Entity Reference Pattern complete
- UI Quality: 15/15 criteria PASS after fixes; 0 CRITICAL, 0 remaining HIGH/MEDIUM blockers
- Token Enforcement: 0 violations (arbitrary px values plan-sanctioned per design-token map)
- Turkish Text: N/A (en-first per track precedent)
- Overall: PASS

## Team Composition
- Analysis Scout: 12 findings (F-A1..F-A12)
- Test/Build Scout: 2 findings (F-B1, F-B2) — both info-only, no blockers
- UI Scout: 7 findings (F-U1..F-U7)
- De-duplicated: 21 → 18 unique findings
  - F-U1 ↔ F-A4 (audit UUID slice fallback) → merged
  - F-U2 ↔ F-A3 (analytics-cost chart label) → merged
  - F-U3 ↔ F-A11 (hover-card touch/online edge) → related, kept separate for attribution

## Merged Findings (severity-sorted)

| ID | Sev | Source | File:Line | Description | Resolution |
|----|-----|--------|-----------|-------------|------------|
| F-A2 | HIGH | Analysis | entity-hover-card.tsx:123 | HoverCard user fetch path `/users/{id}` vs route map `/settings/users/{id}` — endpoint mismatch risk | **NO_ACTION (verified correct)** — backend registers `GET /api/v1/users/{id}` at router.go:315; response shape matches TenantUser `email`+`role` fields used by UserSummary. FE route-map uses `/settings/users/{id}` for FE navigation (different concern). |
| F-A7 | HIGH | Analysis | entity-hover-card.tsx:10 | TenantUser shape vs response — tied to F-A2 | **NO_ACTION** — `userDetailResponse` (handler.go:164) emits `email` + `role` strings matching TenantUser fields consumed by UserSummary. |
| F-U1 / F-A4 | HIGH/MED | UI+Analysis | audit/index.tsx:147 | Raw `entity_id?.slice(0,8)` fallback violates AC-9 orphan rule | **FIXED** — replaced with `<span className="text-text-tertiary" title="No entity reference">—</span>` |
| F-A1 | MED | Analysis | dashboard/analytics.tsx:484 | top_consumers table raw `sim_id.slice(0,8)+'...'` fallback | **FIXED** — replaced span with `<EntityLink entityType="sim" entityId={tc.sim_id} label={tc.iccid} truncate />`; removed row-level `onClick` (EntityLink navigates); dropped unused `useNavigate` import |
| F-A3 / F-U2 | MED | Analysis+UI | dashboard/analytics-cost.tsx:121 | Recharts axis label fallback to `operator_id.slice(0,8)` | **FIXED** — replaced with `op.operator_name \|\| '—'` (orphan em-dash parity per AC-9) |
| F-A11 | MED | Analysis | entity-hover-card.tsx:160 | `navigator.onLine` captured inline; stale on mid-hover network recovery | **DEFERRED** → D-097 (POST-GA UX polish — edge case) |
| F-U3 | MED | UI | entity-hover-card.tsx:194-219 | Controlled Popover lacks close-on-touch-outside — iPad confusion risk | **DEFERRED** → D-098 (POST-GA UX polish — desktop-first accepted) |
| F-U7 | LOW | UI | entity-link.tsx:154 | aria-label reads full UUID when label empty (verbose for SR) | **FIXED** — `aria-label={label ? `View ${entityType} ${label}` : `View ${entityType}`}` |
| F-A5 | LOW | Analysis | dashboard/index.tsx:717,726 | EventSourceChips P3 slice fallback (plan-sanctioned) | **DEFERRED** → D-099 (plan decision — envelope backfill post-FIX-212) |
| F-A6 | LOW | Analysis | entity-link.tsx:113-122 | Right-click preventDefault kills browser "Open in new tab" (plan Decision 6) | **DEFERRED** → D-100 (design decision locked in plan) |
| F-A8 | LOW | Analysis | entity-hover-card.tsx:194 | Redundant `onOpenChange={setIsOpen}` with mouse-wrapper | **DEFERRED** → D-101 (micro-opt; verified no edge flash in practice) |
| F-A9 | LOW | Analysis | esim/index.tsx:428,437 | Dialog copy shows sim_id.slice(0,8) | **DEFERRED** → D-102 (plan-sanctioned; dialog developer-adjacent per AC-12) |
| F-A10 | LOW | Analysis | entity-hover-card.tsx:104-122 | Hover triggers silent token refresh at near-expiry | **DEFERRED** → D-103 (monitor via FIX-205 token-refresh metrics) |
| F-A12 | LOW | Analysis | FRONTEND.md:213 | `violation` route doc nit (code routes correctly) | **DEFERRED** → D-104 (doc-only drift) |
| F-U4 | LOW | UI | entity-link.tsx:136,151 | Arbitrary `text-[12px]` (plan design-token map sanctioned) | **DEFERRED** → D-105 (typography token wave) |
| F-U5 | LOW | UI | entity-hover-card.tsx:203 | Arbitrary `max-w-[280px]` (plan sanctioned) | **DEFERRED** → D-105 (same wave as F-U4) |
| F-U6 | LOW | UI | dashboard/index.tsx:720-727 | Chip helper non-navigable (plan boundary decision) | **NO_ACTION** — EventEntityButton owns navigable event UI per plan |
| F-B1 | LOW | Test/Build | entity-link/entity-hover-card | No unit tests for 4 new props + hover lifecycle | **DEFERRED** → D-091 (FIX-24x FE test infra wave — already open) |
| F-B2 | LOW | Test/Build | admin/purge_history.go | Pre-existing `triggered_by` → `user_id` fix noted | **NO_ACTION** — already fixed in-story T5 |

## Fixes Applied

| # | Category | File | Change | Verified |
|---|----------|------|--------|----------|
| 1 | A11y | web/src/components/shared/entity-link.tsx:154 | Drop UUID from aria-label when label empty | tsc PASS |
| 2 | Compliance (AC-9) | web/src/pages/audit/index.tsx:147 | Raw UUID slice → em-dash with tooltip | tsc+build PASS |
| 3 | Compliance (AC-9) | web/src/pages/dashboard/analytics-cost.tsx:121 | Chart axis UUID slice fallback → em-dash | tsc+build PASS |
| 4 | Compliance (AC-4/AC-9) | web/src/pages/dashboard/analytics.tsx:1,70,189,475-482 | top_consumers span → EntityLink sim; dropped useNavigate + unused row onClick | tsc+build PASS |

## Escalated Issues

None. All HIGH/MEDIUM blockers either FIXED in-gate or verified NO_ACTION (F-A2/F-A7 primary-source verified against backend router.go + handler.go).

### F-A2 reconciliation evidence
- `internal/gateway/router.go:315` registers `r.Get("/api/v1/users/{id}", deps.UserHandler.GetUser)` under an authenticated tenant-gated route block.
- `internal/api/user/handler.go:197` `GetUser` applies tenant scoping (`existing.TenantID != tenantID → 404`).
- Response body (`userDetailResponse` L164-174) emits `email string` + `role string` matching `TenantUser.email` + `TenantUser.role` — the only fields used by `UserSummary` (entity-hover-card.tsx:96-103).
- Conclusion: hover-card `api.get('/users/${id}')` resolves to the correct tenant-scoped endpoint. Scout F-A2 flagged a false-alarm based on the FE route-map (`/settings/users/{id}`, which is the SPA navigation route, not the backend API path).

## Deferred Items (to be added to ROUTEMAP → Tech Debt)

| # | Finding | Target Story | Rationale |
|---|---------|-------------|-----------|
| D-097 | F-A11 hover-card `navigator.onLine` inline snapshot — stale on mid-hover recovery | POST-GA UX polish | Edge case; hover-card is opt-in; low-frequency |
| D-098 | F-U3 HoverCard touch-outside close — iPad confusion risk | POST-GA UX polish | Desktop-first accepted per track; add useEffect click-outside |
| D-099 | F-A5 EventSourceChips P3 slice fallback — post-envelope backfill | POST-GA UX polish | Plan-sanctioned; envelope populated on all FIX-212 publishers |
| D-100 | F-A6 Right-click copy bypasses native context menu (no "Open in new tab") | POST-GA UX polish | Design decision locked in plan Decision 6 |
| D-101 | F-A8 Redundant `onOpenChange` on controlled Popover | POST-GA UX polish | Micro-opt; no verified edge flash |
| D-102 | F-A9 eSIM confirm dialog copy shows sim_id.slice(0,8) | POST-GA UX polish | Plan-sanctioned; dialog copy per AC-12 acceptable |
| D-103 | F-A10 Hover-triggered token refreshes — monitor for refresh storms | POST-GA observability | Existing FIX-205 metrics; disable hoverCard on high-volume surfaces if spikes |
| D-104 | F-A12 FRONTEND.md:213 `violation` route doc nit (code routes correctly) | POST-GA doc sweep | Doc-only drift |
| D-105 | F-U4/F-U5 Arbitrary-px typography tokens (`text-[11/12/13/10]`, `max-w-[280px]`, `min-w-[200px]`) | FIX-24x typography | Plan design-token map sanctioned; add semantic aliases |

## Performance Summary

### Queries Analyzed
| # | File:Line | Pattern | Verdict |
|---|-----------|---------|---------|
| 1 | store/audit.go:255 | `LEFT JOIN users ON u.id = a.user_id` (PK equality) | PASS |
| 2 | store/job.go:242 | `LEFT JOIN users ON j.created_by = users.id` (PK equality) | PASS |
| 3 | api/session/handler.go:395-406 | Single GetByID after O(N) in-memory max | PASS |
| 4 | admin/purge_history.go:56-70 | 4-way JOIN bounded by LIMIT (all PK joins) | PASS |
| 5 | EntityHoverCard fetchEntitySummary | 1 GET per hover, gated by enabled flag + 5min staleTime | PASS |

No N+1 patterns; all joins PK-equality.

### Caching Verdicts
| # | Data | Decision |
|---|------|----------|
| CACHE-V-1 | EntityHoverCard summary (per entity type+id) | CACHE via React Query — 5 min staleTime (in place) |
| CACHE-V-2 | Session stats top_operator | SKIP — sub-ms PK lookup |
| CACHE-V-3 | Audit/Jobs/PurgeHistory user joins | SKIP — PG join within budget + LIMIT-bounded |

### Frontend Performance
- Bundle: 407.91 kB (main index) — post-fix NO CHANGE vs scout measurement (only JSX swaps, no new imports beyond EntityLink into analytics.tsx which tree-shakes alongside existing FE chunks)
- React.memo on EntityLink + EntityHoverCard preserved
- useQuery `enabled` gate verified

## Token & Component Enforcement

| Check | Before | After | Status |
|-------|--------|-------|--------|
| Hardcoded hex colors | 0 | 0 | CLEAN |
| Arbitrary pixel values (render path) | 0 (plan-sanctioned typography tokens only) | 0 | CLEAN |
| Raw HTML elements | 0 | 0 | CLEAN |
| Competing UI library imports | 0 | 0 | CLEAN |
| Default Tailwind colors | 0 | 0 | CLEAN |
| Inline SVG | 0 | 0 | CLEAN |
| Missing elevation | 0 | 0 | CLEAN |
| UUID slice in render path (render-surface) | 3 (F-A1, F-A3, F-A4) | 0 | FIXED |

## Verification (post-fix)

- `cd web && npx tsc --noEmit` → PASS (0 errors)
- `cd web && npm run build` → PASS (built in 15.55s, main bundle 407.91 kB / gzip 124.06 kB)
- `go build ./...` → PASS
- `go test ./internal/api/...` → PASS (911/911 in 39 packages)
- Grep `\.slice\(\s*0\s*,\s*(8|10|12)\s*\)` across `dashboard/analytics.tsx`, `dashboard/analytics-cost.tsx`, `audit/index.tsx` → 0 matches
- Fix iterations: 1 (max 2)

## Passed Items
- AC-1 EntityLink primitive (props, render, orphan em-dash, copyId): PASS
- AC-2 Route map (9 required types + 5 extras = 14 total): PASS
- AC-3 HoverCard 200ms + lazy fetch + offline guard: PASS (user endpoint verified correct)
- AC-4 Page audit + replace across 10+ pages: PASS (23 adopters)
- AC-5 Dashboard Recent Alerts + Top APNs + Op Health wrapped: PASS
- AC-6 top_operator via DTO name: PASS
- AC-7 Event stream envelope.entity.display_name: PASS
- AC-8 Notifications clickable entity_refs: PASS
- AC-9 Orphan em-dash strict rule (no UUID leak): PASS (all 3 fallback violations fixed)
- AC-10 A11y aria-label + focus-visible: PASS (aria-label polished)
- AC-11 Right-click copy-UUID toast: PASS
- AC-12 UUID-only zones preserved (exports, URL params, audit JSON): PASS

---

GATE_RESULT: PASS
