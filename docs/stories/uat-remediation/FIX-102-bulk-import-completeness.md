# FIX-102: Bulk Import Completeness (Audit + Notifications + Policy Auto-Assign)

> Tier 3 (flow completeness) — bulk SIM import runs but silently skips
> audit events, notifications, and policy auto-assignment that UAT-002
> expects. IP allocation was re-scoped to auth-time per STORY-092 and is
> intentionally deferred (see UAT.md D-2 drift edit).

## User Story

As a Tenant Admin importing a 500-row CSV of SIMs, I see per-SIM audit
entries, receive a completion notification, and find each successfully-
imported SIM has the default policy auto-assigned — matching the behaviour
of the single-SIM create/activate path.

## Source Findings Bundled

- UAT Batch 1 report: `docs/reports/uat-acceptance-batch1-2026-04-18.md`
- **F-11 HIGH** — Bulk import writes only 1 `sim.bulk_import` summary audit row for 495 SIMs. Expected: per-SIM `sim.create` + `sim.activate` audit rows
- **F-12 HIGH** — Job completion does not fire a notification via SVC-08 (`notifications` table rows = 0 after import)
- **F-14 HIGH** — All 495 imported SIMs have `policy_version_id = NULL` despite the APN having `default_policy_id` set; single-SIM activate path DOES auto-assign
- **F-13 re-classified** — IP allocation at import time removed from scope (STORY-092 dynamic allocation). UAT.md step 3 and verify checks already updated.
- Odd observation from F-11 investigation: `sim_state_history` has 990 rows for 495 SIMs (2 per SIM, `ordered→active` twice). Plan must triage whether this is a spurious extra write or an intentional "pre-activation" intermediate state

## Acceptance Criteria

### A. Per-SIM audit (F-11)

- [ ] AC-1: Bulk import emits one `sim.create` audit event per imported SIM (with full SIM payload in `data`) via the canonical chain writer (FIX-104)
- [ ] AC-2: Bulk import emits one `sim.activate` audit event per imported SIM that transitions to `active` (whichever state is the terminal post-import state under current STORY-092 semantics)
- [ ] AC-3: Summary `sim.bulk_import` audit row is kept as a single roll-up entry referencing the job_id, in addition to per-SIM rows (not instead of)
- [ ] AC-4: Hash chain remains valid after a 500-SIM import run (`GET /audit-logs/verify` returns `verified:true`)

### B. Completion notification (F-12)

- [ ] AC-5: On job completion (both success and partial-success paths), a notification row is inserted via `SVC-08 / internal/notification` for the triggering user with payload `{job_id, total, success_count, fail_count, error_report_url}`
- [ ] AC-6: Notification type uses an existing template (or new template seeded) with title "Bulk import complete" and body summarising counts
- [ ] AC-7: In-app notification bell `GET /notifications/unread-count` increments after a completed import (verify via API, no UI dependency)
- [ ] AC-8: Notification respects user notification preferences (in-app + webhook channels if configured)

### C. Policy auto-assignment (F-14)

- [ ] AC-9: Bulk import path calls the same policy-resolution function as the single-SIM create path — resolves via APN's `default_policy_id` → tenant's default → no-op if neither
- [ ] AC-10: After import, every successfully-created SIM has `policy_version_id` populated to the APN's default policy version id (not null), verified by SQL query
- [ ] AC-11: If the APN has no default policy, SIM is created with `policy_version_id = NULL` explicitly (no silent error, no job failure)

### D. State-history sanity (spurious double-write)

- [ ] AC-12: Plan triages `sim_state_history` 2-rows-per-SIM observation. If it's spurious, fix the extra write and add a regression assertion "exactly N rows after importing N SIMs where final state matches step-3 expectation". If it's intentional, document the state lineage and update UAT verify checks.

### E. End-to-end green

- [ ] AC-13: Rerun UAT-002 — 10/10 steps pass, 6/6 verify checks pass (with the post-drift expectations updated in UAT.md commit 7175e53)

## Out of Scope

- IP allocation at import time (deferred to auth-time per STORY-092)
- Job runner rewrites / performance improvements
- CSV schema extensions

## Dependencies

- Blocked by: FIX-104 (audit chain, for AC-1/AC-2/AC-4 to be meaningful)
- Blocks: UAT-002 rerun

## Architecture Reference

- Bulk import handler: `internal/api/sim/handler.go` — bulk route
- Job runner: `internal/job/import.go` — line 346 per prior audit
- Audit writer: `internal/audit/` (canonical writer from FIX-104)
- Notification: `internal/notification/` — template lookup + dispatch
- Policy resolver: `internal/api/sim/handler.go` single-create path (grep `default_policy_id`)
- Frontend: `web/src/pages/sims/bulk-import/` — no UI change required

## Test Scenarios

- [ ] Unit: job runner emits expected audit + notification for a 10-SIM CSV
- [ ] Integration: POST 50-row CSV → poll to completion → assert (a) 50 `sim.create` audit rows, (b) 1 notification row for caller, (c) 50 SIMs with `policy_version_id` set
- [ ] Integration: POST CSV with all-invalid rows → 0 SIMs created, still 1 completion notification with `success_count=0`
- [ ] Regression: UAT-002 end-to-end passes

## Effort

L — touches job runner, audit writer (post-FIX-104), notification dispatch, policy resolver. Two Dev iterations realistic.
