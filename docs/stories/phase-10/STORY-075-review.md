# Review Report: STORY-075 — Cross-Entity Context & Detail Page Completeness

> Reviewer: Post-Story Review Agent (Context 2)
> Date: 2026-04-13
> Gate result at review time: PASS (STEP_3)
> Story spec: docs/stories/phase-10/STORY-075-cross-entity-context.md

---

## Check 1: Gate Findings Disposition

All 4 gate fixes verified applied and tested:

| Fix | File | Verification |
|-----|------|-------------|
| Raw `<textarea>` → shadcn `<Textarea>` | web/src/components/shared/related-violations-tab.tsx:307 | tsc clean, build clean |
| +6 user handler tests (GetUser/Activity) | internal/api/user/handler_test.go | go test ./internal/api/user PASS |
| +3 session handler tests | internal/api/session/handler_test.go | go test ./internal/api/session PASS |
| +6 violation handler tests (Get/Remediate) | internal/api/violation/handler_test.go | go test ./internal/api/violation PASS |

No escalated findings. No deferred items from gate.

---

## Check 2: AC Coverage

| AC | Description | Status | Notes |
|----|-------------|--------|-------|
| AC-1 | shared EntityLink component | PASS | entity-link.tsx, React.memo, 10-type union |
| AC-2 | shared CopyableId component | PASS | copyable-id.tsx, masked+reveal, clipboard |
| AC-3 | SIM detail +4 tabs | PASS | policy-history, ip-history, cost, related |
| AC-4 | APN detail +audit/notifications/alerts tabs | PASS | RelatedAuditTab+RelatedNotificationsPanel+RelatedAlertsPanel |
| AC-5 | Operator detail +SIMs/audit/alerts/notifications tabs | PASS | +OperatorSimsTab (addendum) |
| AC-6 | Policy editor +audit/violations/assigned-sims + Clone + Export | PASS | AssignedSimsTab+Clone btn+Export btn (addendum) |
| AC-7 | Session detail page | PASS | sessions/detail.tsx, SoR/policy/quota/audit/alerts tabs, force-disconnect |
| AC-8 | APN detail "Policies Referencing" tab | DEFERRED-D-007 | No backend filter endpoint; client DSL scan fragile. Tracked as D-007 targeting STORY-077. |
| AC-9 | User detail page | PASS | settings/user-detail.tsx, overview/activity/sessions/permissions/notifications, unlock/reset/revoke |
| AC-10 | Alert detail page | PASS | alerts/detail.tsx, overview/similar/audit, ack/resolve/escalate |
| AC-11 | Violation detail page | PASS | violations/detail.tsx, overview/audit, suspend_sim/escalate/dismiss |
| AC-12 | User detail fields (locale/created_by/backup_codes_remaining) | PARTIAL | Not in store.User schema; totp_enabled/email/name/role/state/last_login/locked_until returned. Schema addition is a separate migration. |
| AC-13 | Tenant detail page | PASS | system/tenant-detail.tsx, AnimatedCounter stats, super_admin guard |
| AC-14 | RelatedAuditTab shared component | PASS | expandable JSON diff, footer link, memoized |
| AC-15 | RelatedNotificationsPanel + RelatedAlertsPanel + RelatedViolationsTab | PASS | channel icons, status badges, open/7d tabs, ack mutation |
| AC-16 | Audit log entity_id + actor columns use EntityLink | PASS | audit/index.tsx verified |

**Result: 14/16 PASS, 1 DEFERRED (AC-8), 1 PARTIAL-ACCEPTED (AC-12)**

AC-12 partial is accepted: the three fields (locale, created_by, backup_codes_remaining) are absent from the DB schema/store model. Adding them requires a new migration that is out of scope for a UX story. The UI degrades gracefully when these fields are absent; no regression introduced.

---

## Check 3: Next-Story Impact

**STORY-076 (Universal Search, Navigation & Clipboard)** is the next story; it depends on STORY-075.

- EntityLink is the primary navigation primitive STORY-076 will integrate into its command-palette entity navigation — compatible.
- CopyableId will be reused in search result rows — compatible.
- The 5 new detail pages are now routable targets for the universal search jump-to action — confirmed.
- No interface contracts broken: EntityLink uses a stable `EntityType` string union; extending it for new entity types is additive.
- STORY-077 (Enterprise UX Polish): D-006 (GeoIP) and D-007 (APN Policies Referencing) both target STORY-077 — already tracked.

---

## Check 4: Implementation Quality

- **Architecture pattern adherence**: shared organisms follow build-once-embed-N pattern. All 4 RelatedXxx components are barrel-exported via `web/src/components/shared/index.ts`.
- **Component memoization**: EntityLink and CopyableId wrapped in `React.memo` per plan risk mitigation. Verified in source.
- **Loading/empty/error states**: all 4 shared organisms implement skeleton, empty (`<EmptyState>`), and error fallback branches — verified by static inspection.
- **Shadcn primitives only**: post-fix 0 raw HTML elements. All interactive elements use Button, Dialog, Textarea, Tabs, Table, Badge from shadcn/ui.
- **Design token compliance**: 0 hex matches, 0 arbitrary px values, 0 default Tailwind grays in STORY-075 files. bg-surface, text-text-primary/secondary/tertiary, shadow-card, rounded-[10px]/[6px] applied consistently.
- **TanStack Query caching**: staleTime 30s on related lists, default 5min on detail hooks. Keys scoped by entity_id + entity_type — no redundant fetches.

---

## Check 5: Test Coverage Delta

| Layer | Before | After | Delta |
|-------|--------|-------|-------|
| Go tests (full suite) | 2659 | 2675 | +16 |
| BE handler tests (user+session+violation) | 56 | 72 | +16 |
| FE component smoke tests | 2 | 5 | +3 (entity-link, copyable-id, related-audit-tab) |

All 2675 Go tests PASS with `-short`. Web tsc --noEmit PASS. Web build PASS (27 chunks).

---

## Check 6: API Documentation Completeness

5 new endpoints added to router.go in this story (confirmed in STEP_2 DEV TASK 1):

| Endpoint | Handler | Story |
|----------|---------|-------|
| GET /api/v1/sessions/{id} | SessionHandler.Get | STORY-075 |
| GET /api/v1/users/{id} | UserHandler.GetUser | STORY-075 |
| GET /api/v1/users/{id}/activity | UserHandler.Activity | STORY-075 |
| GET /api/v1/policy-violations/{id} | ViolationHandler.Get | STORY-075 |
| POST /api/v1/policy-violations/{id}/remediate | ViolationHandler.Remediate | STORY-075 |

→ **Assigned API-256..260** in api/_index.md (see Check 14 doc updates).

---

## Check 7: Database Impact

No new migrations in this story. All queries use existing tables (sessions, users, audit_logs, notifications, alerts, policy_violations) via existing store methods extended with new GetByID paths. No schema drift.

---

## Check 8: Performance

- Session enrichment: 3 bounded GetByID lookups per session (sim+operator+apn) — O(1), not N+1. List endpoint paginates; enrichment runs per page, not per row globally.
- Related audit: GIN index on `(tenant_id, entity_id)` — fast lookup, no full-table scan.
- Related data hooks use paginated queries (limit ≤ 50).
- No unbounded scans introduced.

---

## Check 9: Security & Compliance

- Tenant RLS: all 5 new handlers enforce `tenantID == entity.TenantID`; cross-tenant returns 404 (not 403, to prevent existence leak).
- Audit emission: `violation.remediated`, `violation.escalated`, `violation.dismissed` all emit audit events via `apierr.WriteSuccess` flow — verified.
- RBAC: session/violation routes behind `sim_manager` gate; user detail behind `tenant_admin` group; tenant detail behind `super_admin` — verified in router.go.
- Remediate suspend_sim path calls `simStore.Suspend` which enforces existing state transition guard (409 on invalid transitions).

---

## Check 10: Error Code & Envelope Compliance

All 5 new handlers use `apierr.WriteSuccess` / `apierr.WriteList`. No raw `json.NewEncoder` or `http.Error` calls introduced. Envelope format: `{ status, data, meta?, error? }`. Verified by gate Pass 2.

---

## Check 11: Frontend Routing

5 new routes added to `web/src/router.tsx`:

| Route | Component |
|-------|-----------|
| /sessions/:id | sessions/detail.tsx |
| /settings/users/:id | settings/user-detail.tsx |
| /alerts/:id | alerts/detail.tsx |
| /violations/:id | violations/detail.tsx |
| /system/tenants/:id | system/tenant-detail.tsx |

All routes are lazy-loaded (React.lazy + Suspense per G-014). Verified in STEP_2 DEV TASK 14.

---

## Check 12: AC-8 Disposition — APN "Policies Referencing"

**Situation**: AC-8 requires APN detail to show a "Policies Referencing" tab that queries `policies WHERE compiled DSL references this APN name`. No backend endpoint exists for DSL-content filtering; client-side substring scan of policy DSL would be unreliable and fragile (DSL can reference APN by name variant, whitespace, or comment form).

**Decision**: Routed to STORY-077 (Enterprise UX Polish) as tech debt item **D-007**.

**Rationale**:
1. The backend needs a new store query: `WHERE dsl_compiled ILIKE $1` on `policy_versions` table, with appropriate GIN/trigram index.
2. STORY-077 already targets enterprise UX polish and has the right scope for a new filtered endpoint + APN detail tab enrichment.
3. Omission is documented; no silent regression.

**D-007 added to ROUTEMAP Tech Debt table** (see Check 14).

---

## Check 13: Decisions Captured

| ID | Decision |
|----|----------|
| DEV-214 | Shared organism pattern for cross-entity related data (RelatedXxx components): build once, embed N times |
| DEV-215 | EntityLink uses a string-literal TypeScript union for EntityType; route map is inline const (not external config) for zero-runtime-overhead |
| DEV-216 | Remediation actions (suspend_sim / escalate / dismiss) are semantically distinct from acknowledgment (ack); wired to POST /policy-violations/:id/remediate with `action` discriminator |

→ **DEV-214, DEV-215, DEV-216 added to decisions.md** (see Check 14).

---

## Check 14: Documentation Updates Applied

| Doc | Change | Verified |
|-----|--------|---------|
| docs/ARCHITECTURE.md | Scale: 198→203 APIs (+5) | Edit applied |
| docs/architecture/api/_index.md | API-256..260 added; Sessions section +1 row; Auth & Users section +2 rows; new Policy Violations section +2 rows; footer total 198→203 | Edit applied |
| docs/GLOSSARY.md | 4 new terms: Entity Link, Copyable ID, Cross-Entity Context, Remediation Action | Edit applied |
| docs/SCREENS.md | SCR-170..174 added; header 59→64 | Edit applied |
| docs/USERTEST.md | STORY-075 section added (16 scenarios) | Edit applied |
| docs/brainstorming/decisions.md | DEV-214, DEV-215, DEV-216 added | Edit applied |
| docs/ROUTEMAP.md | STORY-075 DONE, counter 17/22→18/22 (3 places), current story updated, D-007 added, change log entry added | Edit applied |

---

## Summary

- **AC coverage**: 14/16 PASS, 1 DEFERRED (AC-8 → D-007 → STORY-077), 1 PARTIAL-ACCEPTED (AC-12 user schema fields)
- **Tests**: 2675/2675 PASS (+16 net-new story tests)
- **Build**: Go clean, web tsc clean, pnpm build PASS
- **Token enforcement**: ALL CLEAR (0 hex, 0 raw HTML post-fix, 0 competing libs)
- **Next unblocked**: STORY-076 (Universal Search) — all EntityLink/detail-page prerequisites satisfied
- **Tech debt added**: D-007 (APN Policies Referencing tab, target STORY-077)
- **Decisions added**: DEV-214, DEV-215, DEV-216
- **APIs added**: API-256..260 (203 total)
- **Screens added**: SCR-170..174 (64 total)

**Overall: PASS**
