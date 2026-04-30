# FIX-219: Name Resolution + Clickable Cells Everywhere (Global Audit)

## Problem Statement
UUIDs appear raw in multiple UI surfaces even when backend provides names (FIX-202 coverage):
- "Top: 20000000" (operator UUID instead of Turkcell) — F-158 Sessions stats
- "Created By: 00000000" (user UUID instead of admin@argus.io) — F-194 Jobs
- "fe482d2a-ed7" (SIM UUID prefix) — F-163 Top SIMs violation
- "View user 00000000…000010" — F-204 Audit log
- Operator in eSIM list: "20000000" — F-173

Pattern: Backend returns `entity_name` + `entity_id` in DTOs (post-FIX-202), but FE renders UUID because:
1. Column bound to wrong field (uses `operator_id` instead of `operator_name`)
2. Name not clickable — no link to detail page
3. No consistent component for entity references — copy-paste divergence

## User Story
As a user, I want every entity reference across Argus UI to display the human name and be clickable (navigate to entity detail). UUIDs should only appear in developer-facing contexts (debug pane, logs, export).

## Architecture Reference
- Shared component: `web/src/components/ui/entity-link.tsx` (NEW)
- Convention: DTO delivers `{id, name}` pair; UI renders link
- Scope: ~15 list pages + detail pages + notification items + events

## Findings Addressed
- F-14 (global pattern)
- F-21 (session events UUIDs)
- F-31 (dashboard refs)
- F-67 (operator detail refs)
- F-75, F-78 (IP Pool reserved_by)
- F-158 (sessions stats "Top: UUID")
- F-163 (violations Top SIMs UUID prefix)
- F-173 (eSIM list operator UUID)
- F-194 (Jobs Created By UUID)
- F-204 (Audit user UUID)

## Acceptance Criteria

### Shared EntityLink component
- [ ] **AC-1:** New `web/src/components/ui/entity-link.tsx`:
  ```tsx
  <EntityLink 
    type="sim|operator|apn|policy|user|session|tenant|ippool|esim_profile" 
    id={uuid} 
    name={displayName}
    showIcon={true}   // optional
    hoverCard={true}  // optional — lazy fetch basic info on hover
  />
  ```
  - Renders: `<icon> <a href="/<type>s/{id}">{name || "—"}</a>`
  - Null/undefined `name` → "—" (no link; tooltip shows raw ID for debugging)
  - ClickableCell wrapper handles right-click → copy ID to clipboard

- [ ] **AC-2:** Component maps type → route:
  - `sim` → `/sims/{id}`
  - `operator` → `/operators/{id}`
  - `apn` → `/apns/{id}`
  - `policy` → `/policies/{id}`
  - `user` → `/settings/users/{id}`
  - `session` → `/sessions/{id}`
  - `tenant` → `/system/tenants/{id}`
  - `ippool` → `/settings/ip-pools/{id}`
  - `esim_profile` → `/esim?profile_id={id}` (list with filter)

- [ ] **AC-3:** Hover card (optional, lazy) shows basic summary from cached data:
  - Operator: code, MCC/MNC, health status chip
  - SIM: ICCID, state, APN
  - User: email, role
  - 200ms delay before fetch; cancel on mouseleave

### Page-by-page audit + apply
- [ ] **AC-4:** Audit list pages for UUID display; replace with EntityLink:
  - `/sims` list — Operator column, APN column (ensure FIX-202 DTO has names)
  - `/sessions` — Operator, APN (DTO enrich)
  - `/violations` — Policy, Operator, SIM (F-163, F-164 rows)
  - `/esim` — Operator (F-173), linked SIM by ICCID (F-175)
  - `/jobs` — Created By (F-194)
  - `/audit` — User (F-204)
  - `/alerts` — SIM, Operator, APN references
  - `/topology` — Already uses names, verify clickable
  - `/admin/security-events` — User, Actor
  - `/admin/purge-history` — Actor

- [ ] **AC-5:** Dashboard sections audit:
  - Recent Alerts panel — entity refs
  - Top APNs — name + link
  - Operator Health Matrix — already name-based, verify clickable

- [ ] **AC-6:** Sessions stats widget (F-158): "Top: Turkcell (22 sessions)" with link to operator detail — not raw UUID.

- [ ] **AC-7:** Live Event Stream (FIX-213 scope aligned):
  - Event entity renders via EntityLink
  - Consumes FIX-212 envelope `entity: {type, id, display_name}`

- [ ] **AC-8:** Notifications list (F-209 scope) — message body entity refs clickable. Backend template includes `{{.Entity.Link}}` resolvable to link.

### Orphan handling
- [ ] **AC-9:** Null entity (post-FIX-206 orphans cleaned) rendering: "—" + tooltip "Entity reference broken" + optional warning icon. Does NOT crash.

### Accessibility
- [ ] **AC-10:** EntityLink accessible — aria-label with full entity name + type, keyboard navigable, focus visible, screen reader announces type ("link: operator Turkcell").

### Copy-to-clipboard
- [ ] **AC-11:** Right-click on EntityLink → context menu option "Copy ID to clipboard" — lets power users grab UUID for debug/API use without showing it in primary UI.

### UUID-only zones (allowed)
- [ ] **AC-12:** UUID raw display ALLOWED in:
  - Export CSV/JSON (full machine-readable)
  - Developer debug pane (Shift+D toggle — future)
  - Audit log JSON dump (already ok)
  - URL query strings (unavoidable)
  - **NOT allowed:** main table cells, detail page labels, notification body, event stream cards

## Files to Touch
- `web/src/components/ui/entity-link.tsx` (NEW)
- `web/src/components/ui/entity-hover-card.tsx` (NEW)
- All 15+ list pages — replace UUID spans with EntityLink
- `web/src/types/event.ts` — EntityRef type (FIX-212 aligned)
- `docs/FRONTEND.md` — EntityLink convention section

## Risks & Regression
- **Risk 1 — DTO missing name field:** For each page, verify backend DTO has `<entity>_name` before this story, or coordinate with FIX-202.
- **Risk 2 — Hover card fetches flood backend:** Debounce + cache; disable if no network.
- **Risk 3 — Link direction wrong for 10K rows:** Virtual scroll (FIX-236) still works; links don't fetch until rendered.
- **Risk 4 — Existing tests assert UUID text:** Update test selectors to use name.

## Test Plan
- Unit: EntityLink renders correctly for all 9 types
- Unit: null name → "—" no crash
- Browser: audit 15 list pages — zero UUID prefixes visible except in debug/export
- Browser: click any EntityLink → correct detail page opens
- Accessibility: keyboard tab through EntityLinks + screen reader

## Plan Reference
Priority: P2 · Effort: M · Wave: 5 · Depends: FIX-202 (backend DTO enrichment)
