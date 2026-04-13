# STORY-058: Frontend Consolidation & UX Completeness

## User Story
As a frontend maintainer and end user, I want shared utilities extracted, a router-level error boundary, code splitting, and all the Phase 8 review findings closed, so that the portal is DRY, resilient, fast, and feature-complete.

## Description
Phase 8 story reviews flagged 7 consecutive stories without an `ErrorBoundary`, growing duplication of `Skeleton` (11 files), `RAT_DISPLAY` (7 files), `formatBytes`/`InfoRow`/`stateVariant`/`stateLabel`, a 425KB gzipped single bundle, WS cache ignoring filtered views, missing filter UIs, and multiple small UX gaps. Close all of them in one disciplined pass.

## Architecture Reference
- Packages: web/src/components/ui, web/src/lib, web/src/layouts, web/src/router, web/src/hooks, web/src/pages
- Source: docs/stories/phase-8/STORY-041..050-review.md (all 10 Phase 8 reviews), docs/reports/phase-8-gate.md, docs/reports/phase-9-gate.md (chunk warning)

## Screen Reference
- All SCR-* that render SIM rows, RAT badges, byte sizes, or tabbed detail panes.
- SCR-050 (Live Sessions), SCR-080 (Jobs), SCR-070 (eSIM), SCR-090 (Audit), SCR-021 (SIM Detail), SCR-030 (APN List), SCR-020 (SIM List), SCR-062 (Policy Editor), SCR-113 (Notification Settings).

## Acceptance Criteria
- [ ] AC-1: Shared `Skeleton` component extracted to `web/src/components/ui/skeleton.tsx` and used in all 11 files (zero duplicate local definitions remain).
- [ ] AC-2: Shared `RAT_DISPLAY` + `RATBadge` extracted to `web/src/lib/rat.ts`/`web/src/components/ui/rat-badge.tsx`. All 7 local definitions removed. Single source of truth.
- [ ] AC-3: `formatBytes`, `formatDuration`, `InfoRow`, `stateVariant`, `stateLabel` extracted to `web/src/lib/format.ts` and `web/src/components/ui/info-row.tsx`. `stateLabel` unified — `LOST/STOLEN` label used everywhere (not the inconsistent `LOST` vs `LOST/STOLEN` split).
- [ ] AC-4: Router-level `ErrorBoundary` wraps the main app layout. Per-tab wrappers on SIM Detail (5 tabs) and policy editor (CodeMirror area). Displays friendly fallback with "Try Again" + "Go Home" actions. Integrates with STORY-056 AC-8 (auto reset on navigate).
- [ ] AC-5: Code splitting via `React.lazy()` applied to all route components. Target: largest initial chunk ≤ 250KB gzipped. Vite `chunkSizeWarningLimit` aligned. Suspense fallback uses shared skeleton.
- [ ] AC-6: WebSocket cache updates respect active filters. When user has filter query active, WS events only merge into cache if they match the filter predicate. Otherwise queued or dropped with metric increment.
- [ ] AC-7: eSIM page adds operator filter dropdown. Audit page adds user filter dropdown. Both wired to existing server-side query params.
- [ ] AC-8: Jobs table renders `created_by` column (user name/email resolved via existing users lookup) instead of `created_at`. Type exposes field; UI consumes it.
- [ ] AC-9: Bulk "Assign Policy" from SIM list opens inline dialog (policy picker + dry-run preview + confirm) instead of navigating away. SIM selection preserved throughout flow.
- [ ] AC-10: "Select all in segment" on SIM list selects the entire segment server-side (using `sim_ids` from segment matching) — not just visible rows. Confirmation dialog shows count.
- [ ] AC-11: `aria-label` added to all icon-only buttons (search clear, row actions, back navigation, close buttons). Unused imports removed from sessions, jobs, esim, audit pages. Command palette includes `/settings/notifications` entry with `BellRing` icon. `stolen_lost` state added to `STATE_COLORS` map and Dashboard SIM Distribution chart formatter.

## Dependencies
- Blocked by: STORY-056 (ErrorBoundary reset behavior), STORY-057 (data hooks for SIM detail tabs)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] Unit: No file outside `web/src/components/ui/skeleton.tsx` defines a local `Skeleton`.
- [ ] Unit: `rg "const.*RAT_DISPLAY.*=" web/src/pages` returns 0 matches.
- [ ] E2E: Inject a component error on SIM Detail Sessions tab — only that tab shows fallback, other tabs still work.
- [ ] E2E: Navigate between routes after a crash — new route renders cleanly (from STORY-056 AC-8).
- [ ] Build: `npm run build` — largest initial chunk ≤ 250KB gzipped, no chunk size warnings.
- [ ] E2E: Apply filter `state=active` on Live Sessions — WS `session.started` event for inactive SIM does not appear in table.
- [ ] E2E: eSIM page — select operator from dropdown, list updates via API call (query param in Network tab).
- [ ] E2E: Audit page — select user from dropdown, list filters correctly.
- [ ] E2E: Jobs table — `created_by` column shows user name for jobs created by admin.
- [ ] E2E: SIM list → select 5 SIMs → bulk "Assign Policy" → dialog opens inline, selection count preserved, confirm triggers bulk job.
- [ ] E2E: SIM list → "Select all in segment X" → confirmation dialog shows segment count (not visible row count) → confirm applies to all.
- [ ] Accessibility: axe-core audit on 5 key pages — zero icon-button-without-label violations.

## Effort Estimate
- Size: L
- Complexity: Medium (lots of small changes + one architectural ErrorBoundary + code split)
