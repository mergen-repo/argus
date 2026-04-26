# FIX-245: Remove 5 Admin Sub-pages (Cost, Compliance, DSAR, Maintenance) + Kill Switches → ENV

## Problem Statement
User scope reduction decision (DEV-254, 2026-04-19):
1. **Cost** — M2M platform doesn't need cost subsystem (billing out of scope)
2. **Compliance** (BTK/KVKK/GDPR summary) — regulatory burden low for M2M; reports also removed (FIX-248)
3. **DSAR Queue** — data subject requests rare for M2M (devices ≠ persons); manual export path sufficient
4. **Maintenance** — admin isolated feature; announcements page suffices for communication
5. **Kill Switches** — **MOVE TO ENV VARIABLES** (not just UI removal) with defaults that DON'T block anything; operational emergency still possible via env + restart

Related: `data_portability.ready` notification event removed (DSAR event cleanup).

## User Story
As a product owner, I want 5 admin sub-pages removed to simplify the platform and reduce maintenance burden; Kill Switches moved to environment variables for ops safety net without requiring UI/DB complexity.

## Architecture Reference
- ~15-20 files across FE/BE/DB/CLI
- Kill switch special: keeps interface `IsEnabled(key) bool` but backed by env reader, not DB

## Findings Addressed
- F-313 (5 admin sub-pages)
- User decision DEV-254
- Implies `data_portability.ready` notif event cleanup (part of FIX-237 scope)

## Acceptance Criteria

### Cost page removal
- [ ] **AC-1:** Delete `web/src/pages/admin/cost.tsx`, types, hooks
- [ ] **AC-2:** Backend `/api/v1/admin/cost/by-tenant` removed from router; `ListCostByTenant` handler removed
- [ ] **AC-3:** Cost-related migration columns (if any) deprecated/dropped — verify `20260321000001_sor_fields.up.sql` cost_terms column usage

### Compliance page removal
- [ ] **AC-4:** Delete `web/src/pages/admin/compliance.tsx`, `web/src/pages/compliance/*`, types, hooks
- [ ] **AC-5:** Delete `cmd/argusctl/cmd/compliance.go` CLI command
- [ ] **AC-6:** Compliance rosette/summary removed from UI

### DSAR Queue removal
- [ ] **AC-7:** Delete `web/src/pages/admin/dsar.tsx`, `web/src/hooks/use-data-portability.ts`
- [ ] **AC-8:** Backend `GET /api/v1/admin/dsar/queue` + DSAR processor handler removed
- [ ] **AC-9 (UPDATED by FIX-237):** `data_portability.ready` event taxonomy + template removal — implemented by **FIX-237** (event catalog + seed templates). FIX-245 retains scope: DSAR Admin sub-page UI, hooks, backend handler, store, and CLI command removal. Cross-reference: `docs/stories/fix-ui-review/FIX-237-m2m-event-taxonomy.md` Section 3 Conflict 2 + DEV-503.
- [ ] **AC-10:** `internal/api/compliance/data_portability.go` removed
- [ ] **AC-11:** DSAR-related notification template removed from seed

### Maintenance removal
- [ ] **AC-12:** Delete `web/src/pages/admin/maintenance.tsx`
- [ ] **AC-13:** Backend `GET/POST/DELETE /api/v1/admin/maintenance-windows` + handler removed
- [ ] **AC-14:** `internal/store/maintenance_window.go` removed
- [ ] **AC-15:** Migration `maintenance_windows` table dropped (new forward migration)
- [ ] **AC-16:** **Announcements PRESERVED** — separate page, user explicit: "sol menüdeki maintenance kalkacak", announcements stays

### Kill Switches → env
- [ ] **AC-17:** Refactor `internal/killswitch/service.go`:
  - Remove DB-backed state (no more `kill_switches` table)
  - Replace with env-var reader:
    ```go
    KILLSWITCH_RADIUS_AUTH=on  // (default "on" — permit auth)
    KILLSWITCH_SESSION_CREATE=on  // (default "on" — permit creation)
    KILLSWITCH_BULK_OPERATIONS=on
    KILLSWITCH_READ_ONLY_MODE=off  // (mutations allowed)
    KILLSWITCH_<NAME>=on|off
    ```
  - `IsEnabled(key)` reads env, caches with TTL (no restart for change if sighup reload implemented; otherwise restart required — acceptable for emergency)
  - Default behavior: ALL switches in "permit" position → nothing blocks traffic unless explicitly set
- [ ] **AC-18:** `kill_switches` DB table dropped (new forward migration); admin UI delete
- [ ] **AC-19:** RADIUS hot path `killSwitch.IsEnabled("radius_auth")` continues to work — only implementation changed
- [ ] **AC-20:** Notification service references to kill-switch — work unchanged (interface stable)
- [ ] **AC-21:** `cmd/argus/main.go` — wire env-backed implementation instead of DB-backed service
- [ ] **AC-22:** Documentation:
  - `docs/architecture/CONFIG.md` lists KILLSWITCH_* env vars
  - `docs/operational/EMERGENCY_PROCEDURES.md` (NEW) — how to toggle kill switch via env + restart

### Cross-cutting
- [ ] **AC-23:** Sidebar ADMIN group removes: Cost, Compliance, DSAR, Maintenance, Kill Switches. Remaining: Quotas/Resources (→ FIX-246 merged as Tenant Usage), Security Events, Sessions (FIX-247 removed), API Usage, Purge History, Delivery Status.
- [ ] **AC-24:** Router cleanup — 5 route removals
- [ ] **AC-25:** Full regression gate — all tests pass; no orphan imports; Go build clean

## Files to Touch
**Frontend (~10 files):** pages + hooks + types + sidebar + router
**Backend (~10 files):** handlers + CLI + compliance package + killswitch service refactor
**DB (3 migrations):** drop kill_switches, drop maintenance_windows, (optionally) clean DSAR schema
**Docs:** CONFIG.md, EMERGENCY_PROCEDURES.md

## Risks & Regression
- **Risk 1 — Kill switch env change requires restart:** For emergencies, restart takes 30s — acceptable. Future: SIGHUP reload for live-toggle without restart.
- **Risk 2 — Existing kill_switches DB rows lost:** Document in release notes; prod should verify no switches were actively ON before migration.
- **Risk 3 — Cost/Compliance/DSAR related events:** Clean notif templates; verify no publisher references removed entities.
- **Risk 4 — Maintenance windows referenced by AAA for scheduled downtime:** Verified only test references exist (F-313) — no AAA code coupling.

## Test Plan
- Unit: killswitch env reader returns expected values for various env configs
- Integration: RADIUS with `KILLSWITCH_RADIUS_AUTH=off` rejects Access-Request; `=on` permits
- Regression: all 5 page routes 404 after removal
- Browser: sidebar no longer lists removed items; no broken links

## Plan Reference
Priority: P2 · Effort: L · Wave: 10 · Depends: FIX-237 (event taxonomy — drop data_portability.ready) · Coordinates with: FIX-237 (taxonomy half — DSAR event removed there, DEV-503)
