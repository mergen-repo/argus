# STORY-079: [AUDIT-GAP] Phase 10 Post-Gate Follow-up Sweep

## Source
- Gap Type: LEFTOVER_FINDING + UNDOCUMENTED + RUNTIME_BUG (mixed)
- Doc Reference: Phase 10 Gate report `docs/reports/phase-10-gate.md` (F-1..F-8), Review DEV-191 (STORY-067)
- Audit Report: `docs/reports/compliance-audit-report.md` (2026-04-15)

## Description

Phase 10 Gate PASSED on 2026-04-13 with 8 documented follow-ups (F-1..F-8). These
items were deferred as "non-blocking" for the gate but were never routed to
ROUTEMAP Tech Debt or to a story, so they have been floating as commentary in
`docs/reports/phase-10-gate.md` without an owner. The 2026-04-15 compliance audit
re-verified each item against the current codebase — all eight are still open.
One additional item from the STORY-067 review (DEV-191) is in the same state.

Bundling them into a single post-gate sweep story preserves the zero-deferral
policy that Phase 10 set out with, and closes the loop before the Documentation
Phase begins. The doc-drift fixes (API-262/263 ID collision, missing
`/policy-violations/export.csv` index entry, missing `session.updated` WS event
entry) were auto-applied in the audit commit and are NOT part of this story.

## Acceptance Criteria

- [ ] **AC-1 (F-1)**: Wire `argus migrate` subcommand in `cmd/argus/main.go` so
  `make db-migrate` actually runs migrations instead of silently falling through
  to `serve`. Preferred approach: dispatch on `os.Args[1]` before `config.Load`
  with `serve` / `migrate up` / `migrate down [N]` / `seed [file]` subcommands
  using the existing `golang-migrate` import. `make db-migrate` and
  `make db-migrate-down` must succeed against a fresh volume.
- [ ] **AC-2 (F-2)**: Split migrations that combine `CREATE INDEX CONCURRENTLY`
  with partitioned parents or `ENABLE ROW LEVEL SECURITY` into two files so the
  CONCURRENTLY clause can run outside a transaction on the parent-only index.
  Either drop CONCURRENTLY where it isn't actually required (parent-only indexes
  on LIST-partitioned tables) or emit `CREATE INDEX` (no CONCURRENTLY) for the
  parent and `CREATE INDEX CONCURRENTLY` for each partition individually. Fresh
  deploys (`make db-reset`) must succeed without manual `force`.
- [ ] **AC-3 (F-3)**: Fix `migrations/seed/003_comprehensive_seed.sql` so it
  applies cleanly on a fresh volume. Root-cause the failure observed in the
  Phase 10 gate (likely a FK / partition dependency after STORY-064 RLS rollout).
  `make db-seed` must produce a demo dataset with at least tenants/operators/
  APNs/SIMs/policies populated; the simulator (STORY-082) should be able to
  discover SIMs via seeded data alone.
- [ ] **AC-4 (F-4)**: `/sims/compare` must read `?sim_id_a=<uuid>&sim_id_b=<uuid>`
  query params on mount via `useSearchParams` and auto-populate the two input
  fields. "Compare" button on `/sims` list must navigate with those params.
- [ ] **AC-5 (F-7)**: Add a `/dashboard` route alias in `web/src/router.tsx`
  that redirects to `/` (or registers the same `DashboardPage` element) so
  existing deep-links and bookmarks do not 404.
- [ ] **AC-6 (F-8)**: Silence the transient `"Invalid session ID format"` toast
  on first dashboard paint. Trace the call site in `/auth/sessions/:id` (origin
  is `internal/api/auth/handler.go:375`) and either skip the call when the ID
  is empty/undefined or add a client-side guard in `use-auth-sessions` (or
  wherever it is invoked) before dispatching the toast.
- [ ] **AC-7 (DEV-191)**: Replace the hardcoded `recent_error_5m: 0` in
  `/api/v1/status/details` with a live Prometheus counter query, OR document
  the field as reserved-future-use in the API and suppress it from the response
  until implemented. Current state misleads operators who consume the field.
- [ ] **AC-8 (F-5 — decision)**: Decide Turkish i18n coverage posture and record
  the decision in `docs/brainstorming/decisions.md`. Options: (a) Defer to a
  dedicated localization story post-GA with TR-only scope; (b) ship partial TR
  (topbar + sidebar + toasts) and flag the rest as English-only; (c) drop the
  TR toggle entirely. Do NOT implement the translations themselves here — the
  AC is to pick a path and reflect it in FRONTEND.md / the language toggle
  behavior.
- [ ] **AC-9 (F-6 — decision)**: Decide whether `/policies` warrants a Compare
  button analogous to `/sims/compare`. Record decision in `decisions.md`.
  If YES → spin out a separate story (likely POST-GA). If NO → remove the
  Phase 10 gate note's recommendation and close the follow-up.

## Technical Notes

- Architecture refs: no new endpoints, no new tables
- Related stories: STORY-067 (CI/CD + argusctl — DEV-191 origin), STORY-076
  (Universal Search — adjacent to F-7), STORY-077 (Enterprise UX Polish —
  adjacent to F-4/F-5/F-8), STORY-078 (SIM Compare — owns F-4), STORY-082
  (Simulator — depends on F-3 seed fix for discovery)
- Files to modify:
  - `cmd/argus/main.go` (AC-1)
  - `migrations/*.up.sql` — the CONCURRENTLY/RLS offenders (AC-2)
  - `migrations/seed/003_comprehensive_seed.sql` (AC-3)
  - `web/src/pages/sims/compare.tsx` (AC-4)
  - `web/src/router.tsx` (AC-5)
  - `web/src/pages/dashboard/index.tsx` or `use-auth-sessions` hook (AC-6)
  - `internal/api/system/status_handler.go` + observability recorder (AC-7)
  - `docs/brainstorming/decisions.md` (AC-8, AC-9)
- Leverages:
  - Existing `golang-migrate` dependency (no new libraries for AC-1)
  - Existing `useSearchParams` pattern already used on 7 pages per STORY-070
    (AC-4)
  - Existing `obsmetrics` registry from STORY-065 (AC-7)

## Priority

HIGH — blocks clean Documentation Phase entry. F-1, F-3 block operator
ergonomics on fresh deploys. F-4, F-7, F-8, DEV-191 are small UX / observability
degradations that Phase 10's zero-deferral policy was supposed to eliminate.
F-2 blocks `make db-reset` from working out-of-box. F-5, F-6 are decision-only
ACs (no implementation, just documented posture).

## Effort

M (estimated 2-3 days). Most ACs are one-line or small-file fixes; F-1 and F-2
are the bulk of the work. F-5 and F-6 are decision ACs with no code.
