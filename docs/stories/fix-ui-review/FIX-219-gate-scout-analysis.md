<SCOUT-ANALYSIS-FINDINGS>

## Inventories

### Field Inventory
| Field | Source | Model | API | UI |
|-------|--------|-------|-----|-----|
| sessions.top_operator {id,name,code,count} | AC-6, F-158 | internal/api/session/handler.go L137 topOperatorDTO | GET /sessions/stats | sessions/index.tsx L363 (consumed) |
| job.created_by_name | AC-4, F-194 | internal/store/job.go L43 CreatedByName | GET /jobs list | jobs/index.tsx (consumed) + types/job.ts |
| job.created_by_email | AC-4, F-194 | store/job.go L44 CreatedByEmail | GET /jobs list | types/job.ts + jobs/index.tsx |
| job.is_system | AC-4, F-194 | api/job/handler.go L73, L127/131-138 | GET /jobs list | types/job.ts |
| audit.user_email | AC-4, F-204 | store/audit.go L197 EntryWithUser | GET /audit/logs | types/audit.ts + audit/index.tsx |
| audit.user_name | AC-4, F-204 | store/audit.go L198 EntryWithUser | GET /audit/logs | types/audit.ts |
| purge.actor_email / actor_name | AC-4 admin | admin/purge_history.go L19-20 | GET /admin/purge-history | types/admin.ts |
| esim.operator_name / operator_code | AC-4, F-173 | internal/api/esim/handler.go L93-94 (pre-existing) | GET /esim-profiles | types/esim.ts |
| violation top-SIM iccid/msisdn | AC-4, F-163 | client-side agg (already present) | — | violations/index.tsx |
| entity_refs[].display_name (notifications) | AC-8 | types/notification.ts L20 NotificationEntityRef | GET /notifications | notifications/index.tsx (T11) |
| envelope.entity.{type,id,display_name} | AC-7 FIX-212 | types/events.ts | WS | event-source-chips.tsx + dashboard/index.tsx L712-723 |

### Endpoint Inventory
| Method | Path | Source | Impl Status |
|--------|------|--------|-------------|
| GET | /api/v1/sessions/stats | AC-6 | Enriched — top_operator DTO present (handler.go L405) |
| GET | /api/v1/jobs | AC-4 | Enriched — LEFT JOIN users; CreatedByName/Email + IsSystem |
| GET | /api/v1/audit/logs | AC-4 | Enriched — ListEnriched() LEFT JOIN users; UserEmail/UserName |
| GET | /api/v1/admin/purge-history | AC-4 admin | Enriched — ssh.user_id LEFT JOIN users (bug fix applied) |
| GET | /api/v1/esim-profiles | AC-4 | Pre-existing enrichment verified (OperatorName, OperatorCode) |
| GET | /api/v1/operators/{id} | AC-3 hover | Consumed by EntityHoverCard fetchEntitySummary |
| GET | /api/v1/sims/{id} | AC-3 hover | Consumed by EntityHoverCard |
| GET | /api/v1/apns/{id} | AC-3 hover | Consumed by EntityHoverCard |
| GET | /api/v1/users/{id} | AC-3 hover | Consumed by EntityHoverCard — NOTE: canonical path is `/settings/users/{id}` per ENTITY_ROUTE_MAP; hover card hits `/users/{id}` which may not exist |

### Workflow Inventory
| AC | Step | Chain Status |
|----|------|--------------|
| AC-1 | Render EntityLink with name + icon | OK — entity-link.tsx L86-172 |
| AC-1 | Orphan em-dash when label+id empty | OK — L94-100 (strict rule) |
| AC-1 | Right-click copy UUID | OK — L113-122 (sonner toast) |
| AC-2 | Route map 9 required types | OK + 5 extras (14 total) |
| AC-3 | HoverCard 200ms delay + cancel | OK — entity-hover-card.tsx L157-173 (setTimeout + mouseleave clearTimeout) |
| AC-3 | Lazy fetch (enabled gate) | OK — useQuery enabled={isOpen && supported && isOnline && !!entityId} |
| AC-3 | Offline guard | OK — navigator.onLine check |
| AC-4 | Replace UUID prefixes in 10+ pages | Mostly OK — 23 EntityLink adopters; residuals found (see F-A findings) |
| AC-5 | Dashboard Recent Alerts, Top APNs, Op Health | Verified wrapped (T9 step-log) |
| AC-6 | top_operator via DTO name | OK — session handler + FE L363 uses stats.top_operator.name |
| AC-7 | Event stream envelope.entity.display_name | OK — event-source-chips + dashboard EventSourceChips |
| AC-8 | Notifications entity_refs render as EntityLink | OK — T11 step-log |
| AC-9 | Orphan em-dash (no UUID leak) | OK — L94-100 |
| AC-10 | A11y aria-label, focus-visible ring | OK — L141, L138-140 ring utilities |
| AC-11 | Right-click copy-UUID toast | OK — L113-122 |
| AC-12 | UUID zones preserved | OK (exports, URL params, audit JSON) |

### UI Component Inventory (UI story)
| Component | Location | Arch Ref | Impl Status |
|-----------|----------|----------|-------------|
| EntityLink | web/src/components/shared/entity-link.tsx | FRONTEND.md L178 "Entity Reference Pattern" | Extended in-place; 173 lines |
| EntityHoverCard | web/src/components/shared/entity-hover-card.tsx (NEW) | FRONTEND.md L178 sub-section | 221 lines; controlled Popover |
| shared/index.ts exports | web/src/components/shared/index.ts L3-4 | — | EntityHoverCard + type exported |
| EventEntityButton | event-stream/event-entity-button.tsx | FRONTEND.md boundary | Not touched (boundary preserved) |
| OperatorChip | shared/operator-chip.tsx | FRONTEND.md boundary | Not touched (boundary preserved) |

### AC Summary
| # | Criterion | Status | Gaps |
|---|-----------|--------|------|
| AC-1 | EntityLink primitive (props, render, null→em-dash, copyId) | PASS | — |
| AC-2 | Route map for 9 required types + 5 extras | PASS | — |
| AC-3 | HoverCard 200ms + lazy fetch + offline guard | MOSTLY PASS | F-A2 users endpoint path mismatch |
| AC-4 | Page audit + replace across 10+ pages | MOSTLY PASS | F-A1, F-A3, F-A5 residual slices |
| AC-5 | Dashboard sections | PASS | F-A5 EventSourceChips still uses id.slice(0,8) as P3 fallback (by design) |
| AC-6 | top_operator name rendered | PASS | — |
| AC-7 | Event stream envelope consumption | PASS | — |
| AC-8 | Notifications clickable entity refs | PASS | — |
| AC-9 | Orphan em-dash (strict) | PASS | — |
| AC-10 | A11y | PASS (manual sign-off deferred to Review T12) | Unit tests deferred to D-091 per FIX-215/216/217/218 precedent |
| AC-11 | Right-click copy | PASS | F-A6 toast only, no context menu chrome (plan-documented tradeoff) |
| AC-12 | UUID-only zones preserved | PASS | Justified comments present (ip-pool-detail, apns/detail) |

## Findings

### F-A1 | MEDIUM | gap
- Title: Analytics (top_consumers) table still uses raw `.slice(0, 8) + '...'` for SIM ICCID fallback; no EntityLink
- Location: web/src/pages/dashboard/analytics.tsx L484 (`{tc.iccid ?? tc.sim_id.slice(0, 8) + '...'}` inside a `cursor-pointer` row, no aria-label)
- Description: AC-4 "list pages" + AC-9 (orphan rule — strict em-dash, no UUID leak) are violated here. Row is clickable via `onClick={() => navigate(…)}` but the cell is a plain span, not EntityLink. When `tc.iccid` missing, user sees UUID prefix in the primary UI — exactly what FIX-219 forbids.
- Fixable: YES
- Suggested fix: Replace span with `<EntityLink entityType="sim" entityId={tc.sim_id} label={tc.iccid} />` (orphan em-dash kicks in if both absent). Row-level `onClick` can stay or be removed (EntityLink already navigates).

### F-A2 | HIGH | gap
- Title: EntityHoverCard "user" fetch hits `/users/{id}` but route map points `user` to `/settings/users/{id}`; backend endpoint mismatch likely
- Location: web/src/components/shared/entity-hover-card.tsx L115-118 (`api.get('/users/${entityId}')`)
- Description: ENTITY_ROUTE_MAP routes `user → /settings/users/{id}` in the FE, and the audit handler join uses `users` table — but there is no verification in story evidence that `/api/v1/users/{id}` is a real tenant-scoped endpoint. If the backend only exposes `/api/v1/settings/users/{id}` (or similar), `hoverCard=true` on `user` EntityLinks will 404 silently (renders "Entity not found"). AC-3 says hover card shows user email+role — functionality broken for user entities if endpoint is wrong.
- Fixable: YES
- Suggested fix: Verify `GET /api/v1/users/{id}` exists and is tenant-auth gated. If not, change fetchEntitySummary user case to the actual route (e.g. `/settings/users/{id}` via api.get, or `/admin/users/{id}`). Add a one-line curl sanity check in step-log.

### F-A3 | MEDIUM | gap
- Title: analytics-cost.tsx chart label uses raw UUID slice fallback; also the "by_operator" TableCell path unverified
- Location: web/src/pages/dashboard/analytics-cost.tsx L121 (`name: op.operator_name || op.operator_id.slice(0, 8)`)
- Description: Recharts chart `name` field is used as the X-axis label. When `operator_name` missing, UUID prefix shows on the chart — an AC-4/AC-9 render surface violation (chart label is "primary UI"). Plan T7 claimed "slice-residuals=0" but this one is active.
- Fixable: YES
- Suggested fix: Since recharts can't render JSX as an axis label directly, substitute `op.operator_name || '—'` (orphan em-dash parity). Alternatively use `op.operator_code` as middle fallback.

### F-A4 | MEDIUM | compliance
- Title: audit/index.tsx L147 still renders `entry.entity_id?.slice(0, 8)` in the fallback branch (no entity_type)
- Location: web/src/pages/audit/index.tsx L147
- Description: When `entry.entity_id` is present but `entry.entity_type` empty, falls through to raw slice span. Strictly per AC-9 this should render em-dash (orphan path) or at minimum wrap with CopyableId. This is a table cell — primary UI.
- Fixable: YES
- Suggested fix: Replace with `<span className="text-text-tertiary">—</span>` or render CopyableId. Alternative: gate by also nulling when entity_type empty (render em-dash).

### F-A5 | LOW | gap
- Title: EventSourceChips P3 fallback still uses `id.slice(0, 8)` when no envelope AND no meta name
- Location: web/src/pages/dashboard/index.tsx L717, L726 (+ mirror in components/event-stream/event-source-chips.tsx)
- Description: Priority chain (envelope → meta → slice) matches AC-7 intent, but the P3 fallback is `id.slice(0, 8)` which is still a UUID surface in primary UI. AC-9 strict rule says em-dash — but events stream context is a compromise (plan decision: envelope is authoritative post-FIX-212; orphan chain is acceptable here).
- Fixable: YES (justification comment could be added, or switch to em-dash)
- Suggested fix: Either add `/* UUID ok: envelope-missing fallback only — post-FIX-212 backfill pending */` comment or change to em-dash on slice-branch. Low severity because envelope is populated on all FIX-212-ported publishers.

### F-A6 | LOW | compliance
- Title: Right-click copy bypasses browser native context menu entirely (plan-decided, but users losing "Open link in new tab")
- Location: web/src/components/shared/entity-link.tsx L113-122
- Description: `onContextMenu` always calls `preventDefault()` when `copyOnRightClick` is true. Power users can no longer middle-click-alternative via "Open in new tab" on right-click. This is the documented trade-off (plan Decision 6), but it means users must ctrl+click or middle-click for new-tab navigation — a UX regression vs. default link behavior.
- Fixable: NO (design decision) but worth surfacing
- Suggested fix: Consider a small keyboard hint or 3-second toast text "UUID copied (ctrl+click to open in new tab)" once per session. Or revise to only preventDefault when clipboard succeeds. Escalate if product disagrees with decision.

### F-A7 | MEDIUM | compliance
- Title: EntityHoverCard `users` endpoint: uses `TenantUser` type but imports from `@/types/settings` — verify type fields (email, role) match the response shape of whichever user endpoint is actually hit
- Location: web/src/components/shared/entity-hover-card.tsx L10 `import type { TenantUser } from '@/types/settings'`
- Description: Tied to F-A2. Even if endpoint exists, if response shape is `{id, email, name, role, ...}` but `TenantUser` only has a subset or different casing, `UserSummary` will render `data.role` as undefined. No runtime test covers this.
- Fixable: YES
- Suggested fix: After fixing F-A2, confirm TenantUser.role + email field names match API JSON keys. Optional: add defensive `data.role ?? '—'` fallbacks in UserSummary.

### F-A8 | LOW | performance
- Title: EntityHoverCard onOpenChange={setIsOpen} may conflict with controlled-only mouse events; Popover may toggle open on focus/click unexpectedly
- Location: web/src/components/shared/entity-hover-card.tsx L178 `<Popover open={isOpen} onOpenChange={setIsOpen}>`
- Description: The parent `<div>` handles onMouseEnter/Leave to set `isOpen`. But Popover's own `onOpenChange` will ALSO fire on clicks/focus/ESC inside the PopoverContent, which could set `isOpen=false` mid-hover. Likely fine in practice since PopoverContent has no interactive elements, but passing `onOpenChange` is redundant with the mouse wrapper. Could cause an edge flash where click-inside closes the card unexpectedly.
- Fixable: YES
- Suggested fix: Change to `<Popover open={isOpen} onOpenChange={() => {}}>` or `open={isOpen}` alone (if prop allows). Low priority — verify with manual test.

### F-A9 | LOW | gap
- Title: esim/index.tsx dialog body still shows `.slice(0, 8)` for SIM ID (L428, L437)
- Location: web/src/pages/esim/index.tsx L428, L437
- Description: Plan T6 explicitly preserves dialog copy as OK, and step-log notes `dialog-copy-preserved=esim:L432+L441`. AC-12 does NOT permit UUID slice in dialog copy (only exports, URL strings, audit JSON, debug pane). This is a minor plan/AC divergence — dialog is a confirm modal, developer-adjacent. Either accept plan decision or switch to ICCID (more user-friendly) with orphan em-dash.
- Fixable: YES
- Suggested fix: Replace `actionDialog.profile.sim_id.slice(0, 8)` with `actionDialog.profile.iccid ?? '—'` for user clarity. Low severity — confirm dialog context.

### F-A10 | LOW | security
- Title: EntityHoverCard api.get lacks error-code discrimination — 401s during hover trigger refresh interceptor noise
- Location: web/src/components/shared/entity-hover-card.tsx L104-122 (fetchEntitySummary)
- Description: The existing axios interceptor (web/src/lib/api.ts L40) auto-retries on 401 via refresh endpoint. If a user hovers a link while their token is near-expiry, the hover card triggers a silent refresh — not a bug, but may cause unexpected refresh storms on dashboards with many hovers. Existing credentialing via `withCredentials: true` is correct; no CSRF token (axios handles via cookie). No hard security issue.
- Fixable: NO (existing interceptor design; Decision 6 says hover-card is opt-in per call site so volume stays low)
- Suggested fix: Monitor via existing token-refresh metrics (FIX-205). If refresh spikes correlate with hover pages, disable `hoverCard` on high-volume surfaces.

### F-A11 | MEDIUM | compliance
- Title: Hover card offline guard captures `navigator.onLine` ONCE at render; stale when network recovers mid-session
- Location: web/src/components/shared/entity-hover-card.tsx L144-145 (`const isOnline = typeof navigator !== 'undefined' ? navigator.onLine : true`)
- Description: `isOnline` is computed inline in the component body, so it re-computes on every render. This is actually OK (hover triggers re-render via state change), but still carries a subtle risk: if `navigator.onLine` was `false` at hover time but flipped to true during the 200ms delay, the query would be gated off with no retry. Opposite is fine (query won't fire if offline at open time).
- Fixable: YES
- Suggested fix: Use `window.addEventListener('online'/'offline')` via a `useSyncExternalStore` or add an explicit online-state hook. Low severity — hover-card is opt-in and offline mode is edge case.

### F-A12 | LOW | gap
- Title: `violation` type in ENTITY_ROUTE_MAP routes to `/violations/{id}` but plan/routemap note "(tooltip only — no route today)" — route exists but detail page may not
- Location: web/src/components/shared/entity-link.tsx L49 and FRONTEND.md L213
- Description: Conflict between entity-link.tsx (has violation route) and FRONTEND.md docs (says "tooltip only — no route today"). If `/violations/:id` detail page doesn't exist, clicking on a violation EntityLink would 404. web/src/pages/violations/detail.tsx exists (spotted in existing-callers list), so likely fine — but doc should be corrected.
- Fixable: YES
- Suggested fix: Update FRONTEND.md L213 to `violation → /violations/{id}`. Verify violations/detail.tsx is routed in App router.

## Non-Fixable (Escalate)

None. All findings fixable in-session by Team Lead.

## Performance Summary

### Queries Analyzed
| # | File:Line | Pattern | Issue | Severity |
|---|-----------|---------|-------|----------|
| 1 | internal/store/audit.go L255 | LEFT JOIN users on PK users.id | None — O(page-size × 1) | PASS |
| 2 | internal/store/job.go L242 | LEFT JOIN users ON j.created_by = users.id | None — users.id is PK | PASS |
| 3 | internal/api/session/handler.go L395-406 | Top-operator computation: O(N) scan over by_operator map + single GetByID lookup | PASS — only one extra DB call per stats request | PASS |
| 4 | internal/api/admin/purge_history.go L56-70 | 4-way JOIN (sim_state_history, sims, tenants, users) | Good — tenants/users both PK joins; indexed on ssh.to_state filter; bounded by LIMIT | PASS |
| 5 | EntityHoverCard fetchEntitySummary | 1 GET per hover (operator/sim/apn/user) | Gated by enabled flag — no fires on mount | PASS (opt-in per call site) |

No N+1 patterns detected. All enrichment joins use primary-key equality (O(1) per row). Sessions stats top-operator does exactly one extra operator store lookup after the in-memory max computation.

### Caching Verdicts
| # | Data | Location | TTL | Decision |
|---|------|----------|-----|----------|
| CACHE-V-1 | EntityHoverCard summary (operator/sim/apn/user by id) | React Query (client) | 5 min staleTime | CACHE — ok, already in place |
| CACHE-V-2 | Session stats top_operator lookup (server-side) | N/A (single per-request request) | — | SKIP — sub-ms PK lookup |
| CACHE-V-3 | Audit ListEnriched / Jobs / PurgeHistory per-row user join | N/A (PG join fast, LIMIT 50-100) | — | SKIP — within budget |

### Frontend Performance
- Bundle: EntityLink + EntityHoverCard inline import is fine; EntityHoverCard mounted only when `hoverCard={true}`. Icons are named lucide imports (tree-shakeable).
- Memoization: both components wrapped in React.memo (entity-link.tsx L86, entity-hover-card.tsx L141). handleContextMenu uses useCallback.
- useQuery in EntityHoverCard: `enabled` gate ensures no fetch on render; only on hover (200ms delay). Verified.
- React Query key: `['entity-summary', entityType, entityId]` — shared across call sites for the same entity (good: dedupes concurrent hovers on same row).
- No virtualization/lazy-loading concerns; no images.

### API Performance
- Payload deltas: job +2 string fields, audit +2 strings, session-stats +1 object (id+name+code+count) → all sub-100-byte additions. Negligible.
- Pagination preserved (audit/jobs cursor-based; purge-history LIMIT-bounded).
- No compression changes needed.

## Summary
- 12 findings: 0 CRITICAL, 2 HIGH (F-A2 user endpoint path, F-A7 tied to A2), 5 MEDIUM (A1, A3, A4, A7, A11), 5 LOW (A5, A6, A8, A9, A10, A12).
- Primary risk: F-A2 — hover-card user fetch endpoint path mismatch. Needs quick curl verification before Review passes.
- Back-compat verified: all 12 pre-existing EntityLink callers use `entityType`/`entityId`/`label` — plan Decision 1 held.
- Purge-history bug fix (`triggered_by` → `user_id`): verified correct. `sim_state_history` has both `triggered_by VARCHAR(20)` and `user_id UUID` columns (migrations/20260320000002_core_schema L206-207). Old code joining `triggered_by` (VARCHAR values like 'admin', 'api', 'system') against `users.id` (UUID) would never match → always returned empty actor_email. New code joins correctly on `ssh.user_id = u.id`.

</SCOUT-ANALYSIS-FINDINGS>
