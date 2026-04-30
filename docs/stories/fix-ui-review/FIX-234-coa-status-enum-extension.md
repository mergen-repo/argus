# FIX-234: CoA Status Enum Extension + Idle SIM Handling + UI Counters

## Problem Statement
Current `policy_assignments.coa_status` enum: `pending | acked | failed`. Gap: when a SIM is assigned a new policy but has no active session, there is nothing to send CoA to — status stays `pending` forever, polluting metrics and obscuring real issues.

**Observed (F-141..F-146 rollout testing):**
- v3 rollout completed, 5 SIMs assigned to v3, all 5 `coa_status='pending'`
- Because v3 SIMs had no active sessions, CoA dispatcher couldn't push anything
- Pending never resolves → SRE cannot distinguish "waiting" from "stale-forever"

## User Story
As an SRE, I want `coa_status` to distinguish "no active session" (skipped appropriately) from "pending a CoA that should succeed soon" so alerts on stuck CoAs only fire for true problems.

## Architecture Reference
- DB: `policy_assignments.coa_status` enum
- Backend: `internal/policy/rollout/service.go::sendCoAForSIM` status propagation
- UI: Rollout panel (FIX-232) + SIM detail

## Findings Addressed
- F-144 (coa_status='pending' permanent for idle SIMs)
- Rollout CoA pipeline observability gaps

## Acceptance Criteria
- [ ] **AC-1:** DB migration: extend `coa_status` enum with: `queued` (delivery in-flight), `no_session` (no active session to push to — idle SIM), `skipped` (policy rule said skip CoA — e.g., low-priority change).
- [ ] **AC-2:** Final enum: `pending | queued | acked | failed | no_session | skipped`.
- [ ] **AC-3:** `sendCoAForSIM` logic:
  - If sessionProvider returns empty session list → set `coa_status='no_session'` (not pending)
  - If dispatch succeeds → `acked`
  - If dispatch fails after retries → `failed`
  - If queued but processing → `queued`
  - `pending` ONLY for just-inserted, not-yet-processed rows (transient)
- [ ] **AC-4:** Automatic re-CoA on next session: when SIM with `coa_status='no_session'` starts a new session, auth handler detects pending policy change → fires CoA at session start → status transitions to `acked`. Covered in FIX-212 `session.started` event subscriber.
- [ ] **AC-5:** Rollout panel UI (FIX-232) shows breakdown: "3 acked · 1 queued · 1 no_session · 0 failed". Color-coded progress.
- [ ] **AC-6:** SIM detail policy section shows `coa_status` + last attempt timestamp. If `failed`: shows reason tooltip.
- [ ] **AC-7:** Alert trigger (FIX-209 alerts table): `coa_status='failed'` for > 5min → alert `type=coa_delivery_failed` with severity=high.
- [ ] **AC-8:** Metrics — Prometheus gauge `argus_coa_status_by_state{state}` — tracks distribution.
- [ ] **AC-9:** Docs — `docs/architecture/PROTOCOLS.md` CoA section updated with status lifecycle.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_coa_status_enum_extension.up.sql`
- `internal/policy/rollout/service.go::sendCoAForSIM` — status propagation
- `internal/aaa/radius/server.go` + `internal/aaa/session/manager.go` — session.started handler triggers CoA for pending policy
- `web/src/components/policy/rollout-active-panel.tsx` (FIX-232) — status breakdown
- `web/src/pages/sims/detail.tsx` — coa_status display
- `docs/architecture/PROTOCOLS.md`

## Risks & Regression
- **Risk 1 — Enum migration on existing rows:** Existing `pending` rows may actually be `no_session`; migration script scans and reclassifies based on active_sessions at migration time.
- **Risk 2 — session.started CoA fire race:** Multiple concurrent sessions for same SIM could fire duplicate CoAs. Mitigation: dedup via `policy_assignments.coa_sent_at` — only fire if not recently sent.
- **Risk 3 — Metric cardinality:** Prometheus gauge by status, bounded small enum — OK.

## Test Plan
- Unit: enum transitions for 4 scenarios (idle → no_session, session start → acked, dispatch fail → failed, etc.)
- Integration: assign policy to idle SIM → status=no_session; start session → CoA fires → acked
- Browser: rollout panel breakdown displays correctly

## Plan Reference
Priority: P2 · Effort: S · Wave: 6 · Depends: FIX-209 (alerts — for AC-7), FIX-212 (session.started event), FIX-232 (UI consumer)
