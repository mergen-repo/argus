# FIX-240: Unified Settings Page + Tabbed Reorganization

## Problem Statement
"Settings" group in sidebar has 8 standalone items (users, api-keys, ip-pools, notifications, knowledgebase, reliability, security, sessions). `/settings/security` only has 2 cards (2FA + Backup Codes) — too narrow for standalone page. User preference: consolidate into single `/settings` tabbed page. Also consolidates F-232 (notification preferences overlap between `/notifications` and `/settings/notifications`).

User decision (F-231, 2026-04-19): unified tabbed page, move existing security/reliability/sessions/notifications into tabs.

## User Story
As an admin, I want all account- and system-level settings organized under one tabbed page so I don't hunt through sidebar for each config.

## Architecture Reference
- Route: new `/settings` (index)
- Old routes 301-redirect to `/settings#<tab>`
- Sidebar: reduce SETTINGS group to 3 items (Settings, Users & Roles, Knowledge Base)

## Findings Addressed
- F-231 (unified settings)
- F-232 (notification preference overlap consolidation)
- F-234 (event taxonomy drift — canonical catalog in one place)
- F-235 (Security narrow scope merged)
- F-236 (M2M event taxonomy in Settings — paralel FIX-237)

## Acceptance Criteria
- [ ] **AC-1:** New `/settings` route with tabbed layout:
  ```
  Tab 1: Security      — 2FA + Backup Codes (existing /settings/security content)
  Tab 2: Active Sessions — existing /settings/sessions content (user-centric session list)
  Tab 3: Reliability   — existing /settings/reliability content (circuit breakers, retry policies)
  Tab 4: Notifications — existing /settings/notifications content, UNIFIED (F-232)
  Tab 5: API Keys      — existing /settings/api-keys (optional — keep standalone?)
  Tab 6: Preferences   — placeholder future (theme, timezone, locale)
  ```
- [ ] **AC-2:** **F-232 Notifications unification** — in Tab 4:
  - Single canonical preference matrix
  - Two view modes: **Simple** (default — category toggle) + **Advanced** (full matrix: event × channel × severity)
  - `/notifications` page KEEPS inbox + templates tabs, REMOVES "Preferences" tab (route `/notifications?tab=preferences` redirects to `/settings#notifications`)
  - Event list from canonical catalog endpoint (FIX-212 AC-5 `/events/catalog`) — eliminates F-234 drift
- [ ] **AC-3:** Tab persistence — URL hash (`/settings#security-tab`) + browser back works.
- [ ] **AC-4:** Deep-link: `/settings#notifications-tab` opens directly on that tab.
- [ ] **AC-5:** Old routes `/settings/security`, `/settings/reliability`, `/settings/sessions`, `/settings/notifications` issue 301 → new hash URLs (preserves bookmarks).
- [ ] **AC-6:** Sidebar SETTINGS group reduced:
  - "Settings" (new unified)
  - "Users & Roles" (separate — RBAC, user management is distinct concern)
  - "Knowledge Base" (separate — docs hub)
- [ ] **AC-7:** Tab content lazy-loaded — only active tab's data fetched.
- [ ] **AC-8:** F-233 (Alert Thresholds orphan in Settings/Notifications) **moved OUT** of Settings — to new Alert subsystem UI (FIX-209 scope) as separate concern.
- [ ] **AC-9:** Role-based tab visibility: Tab 1 (Security) visible to all authenticated users (own account); Tab 3 (Reliability — system config) visible to super_admin only; etc.
- [ ] **AC-10:** Placeholders: Tab 6 (Preferences) shows "Coming soon" with roadmap hint — keeps door open for future without crash.
- [ ] **AC-11:** Responsive — tabs collapse to dropdown on <768px.

## Files to Touch
- `web/src/pages/settings/index.tsx` (NEW — unified)
- `web/src/pages/settings/tabs/security.tsx`, `sessions.tsx`, `reliability.tsx`, `notifications.tsx` — extracted from old pages
- `web/src/router.tsx` — new route + redirects
- `web/src/components/layout/sidebar.tsx` — group reduction
- `web/src/pages/notifications/index.tsx` — remove Preferences tab

## Risks & Regression
- **Risk 1 — Bookmarks broken:** AC-5 redirects mitigate.
- **Risk 2 — Role-visibility regressions:** AC-9 tested per role.
- **Risk 3 — Notification preference data migration:** User opt-in state must carry over. Data lives in `notification_preferences` table — unchanged by UI restructure.

## Test Plan
- Browser: navigate tabs, URL hash updates, back button works
- Old URL redirects to new
- Role-based: analyst user sees Security tab but not Reliability

## Plan Reference
Priority: P2 · Effort: M · Wave: 10 · Depends: FIX-212 (event catalog endpoint for AC-2)
