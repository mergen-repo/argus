# FIX-247: Remove Admin Global Sessions UI (Backend Retain)

## Problem Statement
`/admin/sessions` page shows admin **auth sessions** (login tokens) — different from `/sessions` (SIM/RADIUS sessions). The shared name "Sessions" causes user confusion (F-322). User decision (2026-04-19): remove UI-only; keep backend endpoints + store (needed for login/logout/revoke).

Additionally, data shows low value:
- Many rows with `User-Agent: curl/8.7.1` — API test noise
- Force Logout single-row action exists on this page, but per-user revoke works from User Detail (F-244 Revoke All Sessions action covers)

## User Story
As a product decision, I want to remove `/admin/sessions` sidebar item and page to eliminate naming confusion with SIM sessions. Backend auth session tracking remains (required for JWT auth / logout).

## Architecture Reference
- Frontend: `web/src/pages/admin/sessions-global.tsx` (remove)
- Backend: `internal/api/admin/sessions_global.go` + endpoints preserved (optional — can remove if zero callers)

## Findings Addressed
- F-320, F-322, F-323, F-324 (admin sessions page concerns)

## Acceptance Criteria
- [ ] **AC-1:** Delete `web/src/pages/admin/sessions-global.tsx`
- [ ] **AC-2:** Delete `web/src/hooks/use-admin.ts::useActiveSessions` (keep other admin hooks)
- [ ] **AC-3:** Remove `/admin/sessions` entry from `web/src/router.tsx`
- [ ] **AC-4:** Remove sidebar ADMIN group "Sessions" item
- [ ] **AC-5:** **Backend preserved** per user decision:
  - `internal/api/admin/sessions_global.go` handler file — KEEP (dormant, possibly deletable if zero FE callers)
  - Endpoints `GET /api/v1/admin/sessions/active` + `POST /admin/sessions/{id}/revoke` — KEEP (for programmatic callers / future)
  - Auth session store + tables KEEP (required for login/logout)
- [ ] **AC-6:** **Alternative revoke path** — Per-user Revoke All Sessions works via User Detail "More actions" menu (FIX-244 AC-6). Document in release notes.
- [ ] **AC-7:** Verify no other FE pages link to `/admin/sessions` — grep + clean.
- [ ] **AC-8:** Audit: if any handler/cron references `admin/sessions` endpoint — dormant OK; flag for cleanup in future story.
- [ ] **AC-9:** Release note: "Global admin auth session list UI removed; per-user session revoke remains in User Detail page."

## Files to Touch
- Frontend delete + sidebar/router cleanup

## Risks & Regression
- **Risk 1 — Ops muscle memory:** Admins who used this page — document alternative (User Detail).
- **Risk 2 — Backend handler orphaned:** If truly zero callers, future story can remove endpoint. For now keep as safe harbor.

## Test Plan
- Browser: sidebar no longer shows Sessions under ADMIN; `/admin/sessions` 404 (or redirect)
- User Detail: "Revoke all sessions" button works

## Plan Reference
Priority: P2 · Effort: S · Wave: 10 · No dependencies
