# STORY-076: Universal Search, Navigation & Clipboard

## User Story
As a power user managing thousands of SIMs, I want Cmd+K to instantly find any SIM by ICCID, any operator by name, any policy by title — not just navigate routes — with keyboard shortcuts for everything, recent items in the sidebar, and favorites I can pin, so that I reach any resource in under 2 seconds.

## Description
Command palette currently searches routes only ("Dashboard", "Settings"). Users cannot find a specific SIM, APN, operator, or policy from the search bar. No recent items or favorites in sidebar (store exists but not wired). No keyboard shortcuts beyond Cmd+K and Esc. No contextual row action menus on list pages. This story makes navigation frictionless.

## Architecture Reference
- Packages: web/src/components/command-palette, web/src/hooks/use-search (new), web/src/stores/ui (recent/favorites), web/src/components/shared/row-actions (new), internal/api/search (new backend endpoint)
- Source: Phase 10 UX audit (2026-04-11)

## Acceptance Criteria
- [ ] AC-1: **Backend search endpoint** `GET /api/v1/search?q=...&types=sim,apn,operator,policy,user&limit=5` returns grouped results: `{ sims: [{id, iccid, imsi, state, operator_name}], apns: [{id, name, state}], operators: [{id, name, code, health}], policies: [{id, name, state}], users: [{id, email, name, role}] }`. Searches: SIM by ICCID/IMSI/MSISDN substring, APN by name, Operator by name/code/MCC, Policy by name, User by email/name. Tenant-scoped. Rate limited. Max 50ms target with index hints.
- [ ] AC-2: **Command palette entity search.** Extend existing `command-palette.tsx`: input triggers `useSearch(query, debouncedMs=200)`. Results grouped by type with icon + badge: `[SIM] 89...1234 — Active — Vodafone`, `[APN] iot-m2m.argus`, `[Operator] Turkcell (TR)`. Keyboard nav (arrow up/down, Enter to open detail). Empty state: "No results for X." Recent searches shown when input empty. Max 5 per type shown, "View all" link to filtered list page.
- [ ] AC-3: **Keyboard shortcuts system.** Global key handler (useEffect on document):
  - `?` → opens Keyboard Shortcuts Help Modal (all shortcuts listed in table)
  - `/` → focuses command palette search input
  - `g then s` → go to SIMs, `g then a` → APNs, `g then o` → Operators, `g then p` → Policies, `g then d` → Dashboard, `g then j` → Jobs, `g then u` → Audit
  - `Esc` → close any modal/drawer/panel (already works in most places — verify)
  - On list pages: `j/k` → next/prev row highlight, `Enter` → open detail, `x` → toggle select
  - On detail pages: `e` → edit (opens edit modal/form), `Backspace` → back to list
  - On forms: `Cmd+Enter` or `Ctrl+Enter` → submit, `Cmd+S` → save draft
  - Tooltips on buttons show shortcut: "Activate (A)", "Edit (E)", "Save (⌘S)"
- [ ] AC-4: **Recent items.** `useEffect` on every detail page mount → pushes `{entityType, entityId, label, visitedAt}` to `recentItems` store (max 20, deduplicated by entityId). Sidebar "Recent" section shows last 5 with EntityLink + entity type icon. Clicking opens detail page.
- [ ] AC-5: **Favorites.** Star icon (☆/★) on every detail page header. Click toggles favorite in `favorites` store. Sidebar "Favorites" section (above "Recent") shows starred entities. Persist to localStorage (or user_preferences API if available). Limit 20 favorites.
- [ ] AC-6: **Contextual row action menus.** Every list row gets an ellipsis button (⋮) on hover/focus. Dropdown menu per entity type:
  - SIM: View Detail, Copy ICCID, Copy IMSI, Suspend, Activate, Assign Policy, Run Diagnostics, View Audit
  - APN: View Detail, Copy ID, Archive, View Connected SIMs
  - Operator: View Detail, Copy Code, Test Connection, View Health History
  - Policy: View Detail, Clone, Activate Version, View Assigned SIMs
  - Audit: View Entity, Copy Entry ID, Filter by Entity, Filter by User
  - Session: View Detail, Force Disconnect, View SIM, Copy Session ID
  - Job: View Detail, Retry, Cancel, Download Error Report
  - Alert: View Detail, Acknowledge, Resolve, Copy Alert ID
  Keyboard: `Enter` on highlighted row opens menu, arrow keys navigate.
- [ ] AC-7: **Row quick-peek.** On list pages, hover row for 500ms → shows tooltip/popover card with entity summary (3-4 key fields) without navigating away. Click card → opens full detail. Dismissed by moving mouse away or pressing Esc.

## Dependencies
- Blocked by: STORY-075 (detail pages must exist for navigation targets)
- Blocks: STORY-077 (saved views + advanced UX builds on navigation layer)

## Test Scenarios
- [ ] E2E: Open Cmd+K → type "89012" → SIM result appears with ICCID preview → Enter → navigates to SIM detail.
- [ ] E2E: Press `?` → shortcuts modal appears with all shortcuts listed.
- [ ] E2E: On SIM list, press `j` 3 times → 3rd row highlighted → press `Enter` → detail page opens.
- [ ] E2E: Visit SIM detail → sidebar "Recent" shows this SIM → click star → appears in "Favorites" → refresh page → still in favorites.
- [ ] E2E: Hover SIM list row → ellipsis appears → click → dropdown with "Copy ICCID", "Suspend", etc. → click "Copy ICCID" → clipboard contains ICCID.
- [ ] E2E: Hover operator row for 600ms → popover shows name, MCC, health status, SIM count → click → navigates to operator detail.

## Effort Estimate
- Size: L
- Complexity: Medium-High (backend search endpoint + command palette overhaul + keyboard system)
