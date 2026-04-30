# Implementation Plan: FIX-240 — Unified Settings Page + Tabbed Reorganization

## Goal
Collapse 4 standalone `/settings/*` sub-pages (security, sessions, reliability, notifications) into a single tabbed `/settings` page with hash-based deep-linking, client-side redirects from legacy paths, role-based tab visibility, mobile dropdown collapse, and consolidate F-232 notification preference overlap with `/notifications` — sourced from canonical `/api/v1/events/catalog`.

## Story Context
- **Effort:** M · **Priority:** P2 · **Wave:** 10 · **Depends:** FIX-212 (DONE — `/events/catalog` live)
- **Findings:** F-231, F-232, F-233 (move-out), F-234, F-235, F-236
- **Scope:** Frontend only. NO backend changes. NO data migration. `notification_preferences` table untouched (Risk 3).

## Architecture Touch — Existing Patterns Verified

### Routing pattern (audited `web/src/router.tsx`)
- All routes use `lazy()` + `lazySuspense()` wrapper inside the `<DashboardLayout>` children array
- `<Navigate to="..." replace />` is the established redirect pattern (see `components/auth/protected-route.tsx` lines 1, 11, 16)
- React Router v6 — **no separate "redirect"** primitive; use `<Navigate>` element as route element
- **Spec says "301" — interpret as client-side `<Navigate replace>`.** A real HTTP 301 needs nginx config (`deploy/nginx/`); out of scope unless infra explicitly required. Plan documents this distinction.

### RBAC pattern (audited `web/src/components/layout/sidebar.tsx`)
- Role hierarchy: `api_user(1)` < `analyst(2)` < `policy_editor(3)` < `sim_manager(4)` < `operator_manager(5)` < `tenant_admin(6)` < `super_admin(7)`
- `hasMinRole(userRole, minRole)` helper — currently **private** to `sidebar.tsx` (lines 67–75, 168–171)
- User role read via `useAuthStore((s) => s.user?.role)` from `@/stores/auth`
- Action: extract `ROLE_LEVELS` + `hasMinRole` to `web/src/lib/rbac.ts` and re-import in sidebar + new settings index

### Event catalog hook (audited `web/src/hooks/use-event-catalog.ts`)
- `useEventCatalog()` returns `{ catalog, isLoading }` from `GET /events/catalog`
- Already used by `pages/notifications/preferences-panel.tsx` (line 21) — that component is the canonical "Advanced view" matrix
- **Reuse as-is.** No new fetcher needed (AC-2 hard constraint).

### Tabs primitive (audited `web/src/components/ui/tabs.tsx`)
- Compound `<Tabs value onValueChange>` — controlled, takes string value
- **Local state only** — does NOT sync with URL hash. Need new `useHashTab` hook for AC-3/AC-4.
- No mobile collapse — need wrapper component for AC-11.

### Notifications page (audited `web/src/pages/notifications/index.tsx`)
- Currently has 4 tabs: `unread | all | preferences | templates`
- `<NotificationPreferencesPanel>` (preferences-panel.tsx) is catalog-driven (advanced matrix)
- `<NotificationTemplatesPanel>` stays
- Action (AC-2): remove `'preferences'` from union type + remove `<TabsTrigger value="preferences">` + remove `<TabsContent>`. The preferences component itself MOVES into the new Settings → Notifications tab.

### Legacy `/settings/notifications` page anatomy (audited `pages/settings/notifications.tsx`)
THREE distinct concerns — split per advisor guidance:
1. **Channel config** (lines 27–30: email/telegram/webhook/sms enable + `webhookUrl` + `webhookSecret`) → **KEEP** as a sub-section in new Notifications tab (top of tab body)
2. **Hardcoded event subscription matrix** (lines 31–64: `subscriptions[]` with hardcoded `sim.activated`, etc.) → **DELETE** — fully replaced by `<NotificationPreferencesPanel>` (catalog-driven, eliminates F-234 drift)
3. **Alert thresholds** (lines 65–69: `quota_usage`, `error_rate`, `session_count`) → **DELETE** — F-233 says move out. Already exists at `/settings/alert-rules` and `/alerts`. Drop here entirely.

### Files to touch (verified existence)
| File | Action | Notes |
|------|--------|-------|
| `web/src/pages/settings/index.tsx` | CREATE | Tabbed shell, hash routing, RBAC, mobile collapse |
| `web/src/pages/settings/tabs/security-tab.tsx` | CREATE | Lift body of `pages/settings/security.tsx` (323 lines) |
| `web/src/pages/settings/tabs/sessions-tab.tsx` | CREATE | Lift body of `pages/settings/sessions.tsx` (303 lines) |
| `web/src/pages/settings/tabs/reliability-tab.tsx` | CREATE | Lift body of `pages/settings/reliability.tsx` (452 lines) |
| `web/src/pages/settings/tabs/notifications-tab.tsx` | CREATE | New: channel config + Simple view + Advanced view (reuses `<NotificationPreferencesPanel>`) |
| `web/src/pages/settings/tabs/preferences-tab.tsx` | CREATE | "Coming soon" placeholder |
| `web/src/hooks/use-hash-tab.ts` | CREATE | Hash + popstate sync hook |
| `web/src/lib/rbac.ts` | CREATE | Extract `ROLE_LEVELS` + `hasMinRole` |
| `web/src/router.tsx` | MODIFY | New `/settings` route + 4 `<Navigate>` redirects + remove deleted routes |
| `web/src/components/layout/sidebar.tsx` | MODIFY | Reduce SETTINGS group; import `hasMinRole` from `lib/rbac` |
| `web/src/pages/notifications/index.tsx` | MODIFY | Remove `'preferences'` tab; redirect old `?tab=preferences` query |
| `web/src/pages/settings/security.tsx` | DELETE | Replaced by tab |
| `web/src/pages/settings/sessions.tsx` | DELETE | Replaced by tab |
| `web/src/pages/settings/reliability.tsx` | DELETE | Replaced by tab |
| `web/src/pages/settings/notifications.tsx` | DELETE | Replaced by tab |

**Stay standalone (decision per advisor): API Keys.** 567 lines, modal-heavy. Sidebar reduction (AC-6) gives the UX win without forcing it into the tab.

## Design Token Map (UI — MANDATORY)

#### Color tokens (from FRONTEND.md)
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| Page text | `text-text-primary` | `text-white`, `text-[#fff]` |
| Secondary text | `text-text-secondary` | `text-gray-400` |
| Tertiary text | `text-text-tertiary` | `text-gray-500` |
| Card background | `bg-bg-surface` | `bg-[#0C0C14]` |
| Hover background | `bg-bg-hover` | arbitrary |
| Active background | `bg-bg-active` | arbitrary |
| Border | `border-border` | `border-gray-700` |
| Accent (primary action) | `text-accent` / `bg-accent` | `text-blue-500` |

#### Typography tokens
| Usage | Token Class |
|-------|-------------|
| Page title | `text-lg font-semibold` |
| Section title | `text-sm font-semibold` |
| Body | `text-sm` |
| Helper text | `text-xs text-text-tertiary` |

#### Components to REUSE (DO NOT recreate)
| Component | Path | Use For |
|-----------|------|---------|
| `<Tabs>`, `<TabsList>`, `<TabsTrigger>`, `<TabsContent>` | `web/src/components/ui/tabs.tsx` | Tab shell on >=768px |
| `<Select>` | `web/src/components/ui/select.tsx` | Mobile <768px dropdown collapse |
| `<Card>` | `web/src/components/ui/card.tsx` | Tab content panels |
| `<Button>` | `web/src/components/ui/button.tsx` | All buttons |
| `<EmptyState>` | `web/src/components/shared/empty-state.tsx` | "Coming soon" placeholder body |
| `<NotificationPreferencesPanel>` | `web/src/pages/notifications/preferences-panel.tsx` | Advanced view in Notifications tab |
| `<NotificationTemplatesPanel>` | `web/src/pages/notifications/templates-panel.tsx` | (stays in `/notifications`) |
| `useEventCatalog()` | `web/src/hooks/use-event-catalog.ts` | Catalog source — DO NOT add new fetcher |
| `useAuthStore((s) => s.user?.role)` | `web/src/stores/auth.ts` | Read role for RBAC |

## API Specifications
**No new endpoints.** Reuses:
- `GET /api/v1/events/catalog` — already shipped FIX-212; consumed via `useEventCatalog()`
- All existing notification preference / channel / 2FA / sessions / reliability endpoints

## Database
**No DB changes.** Pure UI restructure. `notification_preferences` table unchanged (Risk 3).

## Acceptance Criteria Mapping

| AC | Requirement | Implementation | Verified by |
|----|-------------|----------------|-------------|
| AC-1 | `/settings` tabbed layout (Security / Sessions / Reliability / Notifications / Preferences placeholder) | Task 4 (`pages/settings/index.tsx`) + Tasks 2 (security/sessions/reliability tabs) + Task 3 (notifications tab) + Task 5 (preferences placeholder) | Task 9 test 1 |
| AC-2 | F-232 unification — single canonical preference matrix; Simple + Advanced toggle; catalog from `/events/catalog`; `/notifications` Preferences tab removed | Task 3 (`tabs/notifications-tab.tsx` w/ Simple+Advanced + `<NotificationPreferencesPanel>`) + Task 6 (remove preferences tab from `pages/notifications/index.tsx`) | Task 9 test 2 |
| AC-3 | Tab persistence via URL hash; back button works | Task 1 (`useHashTab` hook with `popstate` listener) | Task 9 test 3 |
| AC-4 | Deep-link `/settings#notifications` opens that tab on cold load | Task 1 (`useHashTab` reads `window.location.hash` on mount) | Task 9 test 4 |
| AC-5 | Old `/settings/{security,reliability,sessions,notifications}` 301-redirect to new hash URLs | Task 7 (`router.tsx`: `<Navigate to="/settings#tab" replace />`) | Task 9 test 5 |
| AC-6 | Sidebar SETTINGS group → 3 items (Settings, Users & Roles, Knowledge Base) | Task 8 (`components/layout/sidebar.tsx`) | Task 9 test 6 |
| AC-7 | Tab content lazy-loaded — only active tab fetches data | Task 2/3 (gate React Query `enabled: tab === 'x'` in each tab component) + Task 4 (don't render inactive `<TabsContent>`) | Task 9 test 7 |
| AC-8 | F-233 alert thresholds MOVED OUT of Settings | Task 3 (delete thresholds block from old notifications.tsx by NOT carrying it to new tab) | Task 9 test 8 (grep absence) |
| AC-9 | Role-based tab visibility (Reliability: super_admin only; Security: all auth) | Task 4 (`hasMinRole` filter on tab definitions array, imported from `lib/rbac.ts`) + Task 0 extract | Task 9 test 9 |
| AC-10 | Tab "Preferences" placeholder "Coming soon" | Task 5 (`tabs/preferences-tab.tsx` using `<EmptyState>`) | Task 9 test 10 |
| AC-11 | Tabs collapse to dropdown on <768px | Task 4 (Tailwind `md:flex` for `<TabsList>` + `md:hidden` `<Select>` fallback) | Task 9 test 11 |

## Wave / Task Decomposition

> Tasks dispatched to fresh Developer subagents. Each task independently verifiable. Pattern refs point to existing files.

### Wave 1 — Foundations (parallel)

#### Task 0 — Extract RBAC helper to `lib/rbac.ts`
- **Files:** Create `web/src/lib/rbac.ts`; Modify `web/src/components/layout/sidebar.tsx` (replace inline helper with import)
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `web/src/lib/severity.ts` — same shape (constants + helper functions)
- **Context refs:** "Architecture Touch > RBAC pattern"
- **What:** Move `ROLE_LEVELS` constant + `hasMinRole(userRole, minRole)` function from `sidebar.tsx` (lines 67–75, 168–171) into `lib/rbac.ts` and export. Also export `MIN_ROLES` enum-like type. In `sidebar.tsx` replace local definitions with `import { hasMinRole, ROLE_LEVELS } from '@/lib/rbac'`. No behavior change.
- **Verify:** `make web-build` passes; sidebar still filters identically (smoke check by inspecting visible groups for an analyst user).

#### Task 1 — `useHashTab` hook
- **Files:** Create `web/src/hooks/use-hash-tab.ts`
- **Depends on:** —
- **Complexity:** medium (reactivity + popstate)
- **Pattern ref:** `web/src/hooks/use-event-catalog.ts` for module shape; React Router `useNavigate`/`useLocation` patterns visible in `pages/operators/compare.tsx`
- **Context refs:** "Architecture Touch > Tabs primitive"
- **What:** Export `useHashTab(defaultTab: string, validTabs: string[]): [tab, setTab]`. On mount, read `window.location.hash.replace('#', '')`; if in `validTabs` use it, else `defaultTab`. `setTab(value)` calls `window.history.pushState(null, '', '#' + value)` (so back button works) and updates state. Add `popstate` event listener to re-read hash on browser back/forward and update state. Cleanup listener on unmount. Return `[tab, setTab]`.
- **Edge cases:** empty hash → defaultTab; invalid hash → defaultTab (do NOT mutate URL); SSR-safe (`typeof window !== 'undefined'` guard not needed in this project but ensure no top-level window access).
- **Verify:** Add unit test (next task list, Task 9) covering: mount with `#sessions` → returns `'sessions'`; setTab updates hash; popstate from another hash updates state.

#### Task 2 — Extract security/sessions/reliability into tab components
- **Files:** Create `web/src/pages/settings/tabs/security-tab.tsx`, `sessions-tab.tsx`, `reliability-tab.tsx`
- **Depends on:** —
- **Complexity:** low (cut-and-paste body, drop page-level header chrome)
- **Pattern ref:** `web/src/pages/notifications/templates-panel.tsx` — example of page body extracted as a panel component
- **Context refs:** "Architecture Touch > Files to touch"
- **What:** For each of `pages/settings/security.tsx`, `sessions.tsx`, `reliability.tsx`: create a sibling tab component that exports a default React component containing the **body** of the existing page (skip the outer page padding/title wrapper if present — the parent settings shell handles that). Each tab component must accept zero props and gate its data hooks with `enabled` flag if the parent passes a `isActive` prop — but to keep it simple and meet AC-7, rely on the parent ONLY rendering the active `<TabsContent>` (current Tabs primitive already does this — line 81 of `tabs.tsx`: `if (selectedValue !== value) return null`). Document this in the tab's top comment.
- **Note:** DO NOT modify the source files yet — copy the JSX/logic. Source files are deleted in Task 7.
- **Verify:** Each tab file compiles standalone; importing it in a smoke test renders the same content as the original page.

### Wave 2 — Notifications tab (after Wave 1, before settings index)

#### Task 3 — Build new Notifications tab (Simple + Advanced + Channel config)
- **Files:** Create `web/src/pages/settings/tabs/notifications-tab.tsx`
- **Depends on:** Task 2 (so all tab files share a folder convention)
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/notifications/preferences-panel.tsx` (Advanced view source) + `web/src/pages/settings/notifications.tsx` (channel config to lift)
- **Context refs:** "Architecture Touch > Legacy /settings/notifications page anatomy", "Design Token Map", "Components to REUSE"
- **What:** Three sections inside one Card-based tab:
  1. **Channel config** (top): Lift `CHANNEL_META`, `webhookUrl`/`webhookSecret` inputs, save button + `useNotificationConfig`/`useUpdateNotificationConfig` hooks from old `pages/settings/notifications.tsx`. **DO NOT** lift `subscriptions[]` (line 31–64) or `thresholds[]` (line 65–69).
  2. **View toggle** (Simple | Advanced) — local state, default `'simple'`. Use a `<TabsList>` mini-tab or two `<Button>` toggles.
  3. **Simple view** (NEW component, internal to this file): Group canonical events from `useEventCatalog()` by `category` field (e.g. `sim`, `session`, `policy`, `system`). Render one `<Checkbox>` per category that bulk-toggles `enabled` for all events in that category. Reuse `useNotificationPreferences` + `useUpsertNotificationPreferences`. Save button shared with Advanced.
  4. **Advanced view**: Render existing `<NotificationPreferencesPanel>` as-is.
- **Tokens:** Use ONLY classes from Design Token Map. Zero hex/px.
- **Components:** Reuse `<Card>`, `<Button>`, `<Checkbox>`, `<Input>`, `<Tabs>` for the inner view toggle.
- **Note:** Invoke `frontend-design` skill for the Simple view layout.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}|\\[[0-9]+px\\]' web/src/pages/settings/tabs/notifications-tab.tsx` returns zero matches. Mounting the tab loads catalog and persists changes via existing API.

### Wave 3 — Shell + integrations (after Wave 1 & 2)

#### Task 4 — Settings index page (tab shell)
- **Files:** Create `web/src/pages/settings/index.tsx`
- **Depends on:** Task 0, Task 1, Task 2, Task 3, Task 5
- **Complexity:** medium
- **Pattern ref:** `web/src/pages/notifications/index.tsx` — same outer page shell + `<Tabs>` + role-aware filtering precedent in `sidebar.tsx`
- **Context refs:** "Acceptance Criteria Mapping > AC-1, AC-7, AC-9, AC-11", "Components to REUSE"
- **What:** Default-export `SettingsPage()`:
  - Read `userRole = useAuthStore((s) => s.user?.role)`
  - Define `TAB_DEFS = [{ key: 'security', label: 'Security', minRole: undefined, Component: SecurityTab }, { key: 'sessions', label: 'Active Sessions', minRole: undefined, Component: SessionsTab }, { key: 'reliability', label: 'Reliability', minRole: 'super_admin', Component: ReliabilityTab }, { key: 'notifications', label: 'Notifications', minRole: undefined, Component: NotificationsTab }, { key: 'preferences', label: 'Preferences', minRole: undefined, Component: PreferencesTab }]`
  - Filter to `visibleTabs = TAB_DEFS.filter((t) => !t.minRole || hasMinRole(userRole, t.minRole))`
  - `[tab, setTab] = useHashTab(visibleTabs[0].key, visibleTabs.map((t) => t.key))`
  - Render outer page header "Settings"
  - **Desktop (>=768px)** — `<Tabs value={tab} onValueChange={setTab}>` with `<TabsList className="hidden md:inline-flex">` containing `<TabsTrigger>` for each visibleTab
  - **Mobile (<768px)** — `<Select className="md:hidden" value={tab} options={visibleTabs.map((t) => ({ value: t.key, label: t.label }))} onChange={(e) => setTab(e.target.value)} />`
  - Below: `<TabsContent value={t.key}>` for each visibleTab, rendering `<t.Component />`
  - Lazy-import each tab component via `React.lazy()` + wrap in `<Suspense fallback={<spinner>}>` to satisfy AC-7's "lazy load" interpretation at module level too
- **Tokens:** Use ONLY Design Token Map classes.
- **Verify:** `grep -E '#[0-9a-fA-F]{3,6}' web/src/pages/settings/index.tsx` returns zero. At runtime: viewport <768px shows dropdown; analyst user does NOT see Reliability tab.

#### Task 5 — Preferences placeholder tab
- **Files:** Create `web/src/pages/settings/tabs/preferences-tab.tsx`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** `web/src/components/shared/empty-state.tsx` — usage examples in `pages/notifications/index.tsx`
- **Context refs:** "Acceptance Criteria Mapping > AC-10"
- **What:** Default-export component rendering `<EmptyState icon={Settings} title="Coming soon" description="Theme, timezone, and locale preferences will live here in a future release." />`. No data fetching.
- **Verify:** Renders without error; no network calls in DevTools.

#### Task 6 — Remove Preferences tab from `/notifications`
- **Files:** Modify `web/src/pages/notifications/index.tsx`
- **Depends on:** Task 3 (so the new home exists)
- **Complexity:** low
- **Pattern ref:** Existing file (delete + redirect)
- **Context refs:** "Acceptance Criteria Mapping > AC-2"
- **What:**
  1. Remove `'preferences'` from `NotificationsTab` union type (line 75 of current file).
  2. Remove `<TabsTrigger value="preferences">Preferences</TabsTrigger>` (line 109).
  3. Remove the `tab === 'preferences'` `<TabsContent>` block (lines 113–117).
  4. Remove `import { NotificationPreferencesPanel } from './preferences-panel'` (line 25).
  5. **Do NOT delete `preferences-panel.tsx`** — Settings → Notifications tab now imports it.
  6. Add a useEffect at top of `NotificationsPage()`: `const [searchParams] = useSearchParams(); useEffect(() => { if (searchParams.get('tab') === 'preferences') navigate('/settings#notifications', { replace: true }) }, [searchParams])` — handles legacy `?tab=preferences` deep-links per AC-2.
- **Verify:** `grep "value=\"preferences\"" web/src/pages/notifications/index.tsx` returns zero. Navigating to `/notifications?tab=preferences` redirects to `/settings#notifications`.

#### Task 7 — Router updates: new route + redirects + delete legacy entries
- **Files:** Modify `web/src/router.tsx`
- **Depends on:** Task 4
- **Complexity:** low
- **Pattern ref:** `web/src/components/auth/protected-route.tsx` — `<Navigate replace>` usage; existing route entries in `router.tsx`
- **Context refs:** "Architecture Touch > Routing pattern", "Acceptance Criteria Mapping > AC-5"
- **What:**
  1. Remove these lazy imports + route entries: `SecurityPage`, `ActiveSessionsPage`, `ReliabilityPage`, `NotificationConfigPage` (lines 50, 52–54, 167, 169–171).
  2. Add lazy import: `const SettingsPage = lazy(() => import('@/pages/settings/index'))`.
  3. Add new route: `{ path: '/settings', element: lazySuspense(SettingsPage) }`.
  4. Add 4 redirect routes (immediately after the `/settings` line):
     ```tsx
     { path: '/settings/security', element: <Navigate to="/settings#security" replace /> },
     { path: '/settings/sessions', element: <Navigate to="/settings#sessions" replace /> },
     { path: '/settings/reliability', element: <Navigate to="/settings#reliability" replace /> },
     { path: '/settings/notifications', element: <Navigate to="/settings#notifications" replace /> },
     ```
  5. Add `import { Navigate } from 'react-router-dom'` to top imports.
  6. Delete files (after redirects in place): `pages/settings/security.tsx`, `sessions.tsx`, `reliability.tsx`, `notifications.tsx`.
- **Verify:** `grep -E "(SecurityPage|ActiveSessionsPage|ReliabilityPage|NotificationConfigPage)" web/src/router.tsx` returns zero. Navigate to `/settings/security` in browser → URL becomes `/settings#security` and Security tab is active.

#### Task 8 — Sidebar SETTINGS group reduction
- **Files:** Modify `web/src/components/layout/sidebar.tsx`
- **Depends on:** Task 0 (rbac extraction), Task 7 (so removed routes exist)
- **Complexity:** low
- **Pattern ref:** Existing nav group definitions (lines 77–166)
- **Context refs:** "Acceptance Criteria Mapping > AC-6"
- **What:** Replace SETTINGS group items (lines 116–125) with:
  ```tsx
  {
    title: 'SETTINGS',
    minRole: 'tenant_admin',
    items: [
      { label: 'Settings', icon: Settings, path: '/settings' },
      { label: 'Users & Roles', icon: Users, path: '/settings/users' },
      { label: 'Knowledge Base', icon: BookOpen, path: '/settings/knowledgebase' },
    ],
  }
  ```
  Add `Settings` to lucide imports if not already present. **Keep**: `/settings/api-keys` route — but remove from sidebar (per advisor: standalone but not in sidebar group; users reach it via Settings index "Related" link or future direct link). Decision: leave API Keys reachable via direct URL only; add a "Related: API Keys" link inside Settings → Security tab as a soft signpost (Task 2 implementation note). **Move "Knowledge Base"** from OPERATIONS group (line 112) to SETTINGS group; remove from OPERATIONS.
- **Verify:** SETTINGS group renders exactly 3 items; OPERATIONS no longer has Knowledge Base; Alert Rules removed from sidebar (live elsewhere via `/alerts`).

### Wave 4 — Tests

#### Task 9 — Test suite
- **Files:** Create `web/src/pages/settings/__tests__/settings-index.test.tsx`, `web/src/hooks/__tests__/use-hash-tab.test.ts`, `web/src/__tests__/settings-redirects.test.tsx`
- **Depends on:** Tasks 1–8
- **Complexity:** medium
- **Pattern ref:** `web/src/hooks/__tests__/` (existing hook tests) + `web/src/__tests__/shared/` (existing component tests)
- **Context refs:** "Acceptance Criteria Mapping" (full table)
- **What:** Cover scenarios:
  1. Mount `<SettingsPage>` with no hash → first visible tab active (AC-1)
  2. Mount with `#notifications` → Notifications tab active (AC-4)
  3. Click another tab → URL hash updates (AC-3)
  4. `popstate` event with new hash → tab switches without remount (AC-3 back button)
  5. `useHashTab` invalid hash → falls back to default; valid hash retained
  6. Render with `analyst` role → Reliability tab NOT visible (AC-9)
  7. Render with `super_admin` role → Reliability tab visible (AC-9)
  8. `<NotificationsTab>` Simple/Advanced toggle works; catalog hook called with `/events/catalog` (mock)
  9. `<NotificationsTab>` does NOT render hardcoded `sim.activated` etc. — only catalog-driven event types (AC-2 + AC-8)
  10. Visit `/settings/security` → React Router redirects to `/settings#security` (AC-5) — test all 4 legacy paths
  11. Visit `/notifications?tab=preferences` → redirects to `/settings#notifications` (AC-2 sub-rule)
  12. Sidebar SETTINGS group renders exactly 3 items for tenant_admin (AC-6)
  13. Viewport <768px (mock matchMedia) → `<Select>` rendered, `<TabsList>` hidden (AC-11)
  14. Inactive tabs do not fetch their data (AC-7) — assert mocked hook NOT called when tab not active
- **Verify:** All tests pass; `cd web && pnpm test settings-index use-hash-tab settings-redirects` green.

## Story-Specific Compliance Rules

- **UI:** Design tokens from FRONTEND.md — zero hardcoded hex/px in any new file
- **API:** No new endpoints; reuse `/events/catalog` (FIX-212), `/notifications/preferences` (existing), `/settings/security/*`, `/settings/sessions/*`, `/settings/reliability/*` (all existing)
- **DB:** Zero migrations
- **RBAC:** Reuse existing `hasMinRole` (extracted to `lib/rbac.ts`); no new permission strings
- **A11y:** `<Select>` mobile fallback must have a `<label>`; `<TabsTrigger>` accessibility comes from primitive
- **i18n:** Use existing `web/src/locales/{en,tr}` keys where available — add new keys for Simple/Advanced toggle if needed (`settings.notifications.viewSimple`, `settings.notifications.viewAdvanced`, `settings.preferences.comingSoon`)

## Bug Pattern Warnings
- **PAT-003 (Turkish chars):** Any Turkish UI text must use proper UTF-8 (no `s` for `ş`). Run `grep -r "ş\|ğ\|ı\|ç\|ö\|ü"` on i18n files after additions.
- **PAT-006 (recurrence #3):** Watch for missed nil-slice → JSON `null` regressions if any backend response is restructured. **N/A here — no backend changes.**
- **PAT-023 (zero-code schema drift):** Hardcoded event lists drift from canonical catalog. **AC-2 directly fixes this** — Notifications tab MUST use `useEventCatalog()`, never a hardcoded list.

## Tech Debt (from ROUTEMAP)
No tech debt items target FIX-240. (Wave 9 closed D-150..D-167; none scoped to settings restructure.)

## Mock Retirement
No mocks directory in this project — N/A.

## Risks & Mitigations

| Risk | Severity | Mitigation |
|------|----------|------------|
| Bookmarks broken (Risk 1 from spec) | Medium | AC-5 client-side `<Navigate>` redirects for all 4 legacy paths + Task 9 test 10 |
| Role-visibility regression (Risk 2 from spec) | Medium | Reuse `hasMinRole` (no duplicated logic) + Task 9 tests 6/7; manual test with analyst, tenant_admin, super_admin accounts |
| Notification preference data loss (Risk 3 from spec) | Critical | Pure UI restructure — `notification_preferences` table untouched; same hooks (`useNotificationPreferences`, `useUpsertNotificationPreferences`) used by both old and new paths; verified `preferences-panel.tsx` is reused as-is |
| `useHashTab` SSR / hydration mismatch | Low | Project is SPA-only (Vite, no SSR); no risk in practice |
| Mobile dropdown a11y regression vs. native tabs | Low | `<Select>` already used elsewhere with proper `<label>`; reuse pattern |
| `popstate` listener leak | Low | Cleanup in useEffect return; tested in Task 9 test 4 |
| API Keys discoverability drops after sidebar removal | Low | Add "Related: API Keys" link in Security tab body (Task 2 note); URL still works |
| Spec-vs-impl semantics for "301" | Low | Plan documents client-side `<Navigate>` interpretation explicitly; no real HTTP 301 expected |

## Quality Gate Self-Check

- [x] All 11 ACs mapped to concrete file paths + functions (see AC mapping table)
- [x] FIX-212 catalog endpoint integration documented — `useEventCatalog()` reused; no hardcoded event lists (Task 3)
- [x] 301 redirect mechanism audited — uses existing `<Navigate replace>` pattern from `protected-route.tsx`; semantic distinction documented
- [x] RBAC mechanism audited — extracts existing `hasMinRole` to `lib/rbac.ts`; no new pattern
- [x] No scope creep — no new alert pages (AC-8 = MOVE OUT/DROP, alerts already at `/alerts`); zero backend; zero data migration
- [x] Wave breakdown enables parallel dispatch — Wave 1 has 3 parallel tasks (Task 0/1/2), Wave 3 has 4 parallel tasks (Task 4 sequential after Wave 1+2; Task 5/6/7/8 mostly parallel)
- [x] Risks section addresses spec Risk 1/2/3 + 5 newly identified risks
- [x] Test plan covers hash routing (3,4), redirects (10), role visibility (6,7), mobile collapse (13), lazy load (14), AC-2 catalog-driven (8,9), AC-6 sidebar (12)
- [x] Min substance — M effort needs ≥60 lines, ≥3 tasks: this plan ~310 lines, 10 tasks (Tasks 0–9)
- [x] Required sections present: Goal, Architecture Touch, Tasks, Acceptance Criteria Mapping
- [x] Embedded specs (no "see X.md")
- [x] Each new-file task has `Pattern ref`
- [x] Each task has `Depends on` + `Context refs`
- [x] No implementation code (only structural sketches)
- [x] Total: 10 tasks across 4 waves (well within 3–8 typical for M, slightly higher due to file moves which are inherently low-complexity)

**Quality Gate result: PASS**
