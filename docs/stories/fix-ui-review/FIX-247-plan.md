# FIX-247 Plan — Combined Dispatch (S Story)

> **Pattern**: For S-effort stories, AUTOPILOT may collapse Plan + Dev + Lint into a single Developer dispatch when scope is unambiguous and the spec serves as the plan. See FIX-247 dispatch (commit 0267a2b).

## Status

**Plan-as-spec**: This story used the spec file `FIX-247-remove-admin-sessions-ui.md` directly as the implementation plan. No separate planner-generated `*-plan.md` artifact was produced because:

1. Effort = **S** — pure FE deletion, no architectural decisions
2. Scope = 9 ACs, all enumerated explicitly in spec (delete 1 page + 1 hook + 1 route + 1 sidebar entry)
3. Backend retention is unambiguous per AC-5
4. No new code, no new dependencies, no migrations
5. PAT-026 sweep is **limited** (UI-only removal, not full-stack feature deletion) — documented in DEV-580

## Combined Wave Structure

Single wave (combined Plan + Dev + Lint):

| Step | Action | Reference |
|------|--------|-----------|
| Plan | Spec served as plan | `docs/stories/fix-ui-review/FIX-247-remove-admin-sessions-ui.md` |
| Dev | 4 FE deletions/edits | commit 0267a2b |
| Lint | tsc + vite + grep verifications | step-log STEP_2.5 |
| Gate | Combined Gate Lead + Reviewer (S-story optimization) | `FIX-247-gate.md` + `FIX-247-review.md` |
| Commit | Single unified commit | 0267a2b |

## Acceptance Criteria Mapping

See spec for full AC text (`FIX-247-remove-admin-sessions-ui.md`). All 9 ACs PASS:

- **AC-1**: `web/src/pages/admin/sessions-global.tsx` deleted
- **AC-2**: `useActiveSessions` + `useForceLogoutSession` hooks removed from `use-admin.ts`
- **AC-3**: `/admin/sessions` route + `AdminGlobalSessionsPage` lazy import removed from `router.tsx`
- **AC-4**: ADMIN sidebar group "Sessions" entry removed; unused `UserCheck` lucide import dropped
- **AC-5**: Backend PRESERVED — `internal/api/admin/sessions_global.go` (6.0K) + 2 routes (`router.go:938-939`) intact
- **AC-6**: Per-user revoke alternative via User Detail page (no FE changes needed)
- **AC-7**: Grep verified zero remaining FE refs to `/admin/sessions`, `useActiveSessions`, `useForceLogoutSession`, `sessions-global`, `AdminGlobalSessionsPage`
- **AC-8**: Dormant BE handler flagged for future cleanup → D-180 routed
- **AC-9**: Release note: "Global admin auth session list UI removed; per-user session revoke remains in User Detail page"

## Risks Addressed

- **Risk 1 — Ops muscle memory**: Documented alternative path (User Detail "Revoke all sessions") in USERTEST.md UT-247-03 + release note
- **Risk 2 — Backend handler orphaned**: Routed to D-180 Tech Debt for future story (truly dormant; safe to leave)

## Tech Debt Routed

- **D-180**: Dormant `internal/api/admin/sessions_global.go` handler + 2 routes — future cleanup story can delete if no programmatic callers identified

## Files Touched (4 + 1 cleanup)

- DELETED `web/src/pages/admin/sessions-global.tsx`
- MOD `web/src/hooks/use-admin.ts`
- MOD `web/src/router.tsx`
- MOD `web/src/components/layout/sidebar.tsx`
- DELETED `job.test` (stray binary cleanup from FIX-238 commit e0059f9)

## Backend Verification (AC-5)

```bash
ls /Users/btopcu/workspace/argus/internal/api/admin/sessions_global.go
# → -rw-r--r-- 6.0K (PRESENT)

grep -n 'sessions/active\|sessions.*revoke' /Users/btopcu/workspace/argus/internal/gateway/router.go
# → router.go:938: r.Get("/api/v1/admin/sessions/active", ...)
# → router.go:939: r.Post("/api/v1/admin/sessions/{session_id}/revoke", ...)
```

## Final Regression Gate

- `pnpm tsc --noEmit` → PASS (0 errors)
- `pnpm build` (vite) → PASS (2.65s)
- `go build ./...` → PASS
- `go vet ./...` → PASS (clean)
- `go test ./internal/api/admin/...` → PASS (cached)
- FE grep → 0 matches for all 5 deletion targets

## Commit

`0267a2b feat(FIX-247): Remove Admin Global Sessions UI (backend retain) (Wave 10 P2 — S)` — 16 files +178/-218
