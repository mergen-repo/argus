# STORY-077: Enterprise UX Polish & Ergonomics

## User Story
As an enterprise user who has used Linear, Datadog, and Jira, I want saved views, undo on destructive actions, inline editing, CSV export on every list, helpful empty states, data freshness indicators, sticky table headers, column customization, form validation as I type, admin impersonation, system announcements, and localization — so that Argus feels like a polished SaaS tool, not a feature dump.

## Description
Enterprise UX audit rated Argus at ~60% UX maturity. Foundations are strong (dark mode, error boundaries, skeletons, WebSocket live updates, SIM comparison). But 16 enterprise-standard UX patterns are missing or partial. This story closes the gap between "promising product" and "enterprise-ready tool."

## Architecture Reference
- Packages: web/src/components/shared/* (new UX components), web/src/hooks/* (new UX hooks), web/src/stores/*, web/src/pages/* (enhancements), internal/api/user (preferences), internal/api/admin (impersonation, announcements), migrations
- Source: Phase 10 enterprise UX audit (2026-04-11)

## Acceptance Criteria
- [ ] AC-1: **Saved views.** "Save View" button on every list page. Dialog: name, auto-save toggle. Stored in `user_views` table (`id`, `user_id`, `page`, `name`, `filters_json`, `columns_json`, `sort_json`, `is_default`, `shared`, `created_at`). Sidebar "My Views" section per page. Click view → filters/columns/sort restored. Share with team (per-tenant visibility). One default view per page per user.
- [ ] AC-2: **Undo destructive actions.** After: bulk suspend, bulk terminate, delete policy, delete segment, revoke API key — show toast: "X items suspended. [Undo]" with 10-second countdown. Click Undo → calls backend rollback endpoint → restores previous state. Backend: `POST /api/v1/undo/:action_id` (stored in Redis with 15s TTL, contains inverse operation). If not undone, TTL expires and action is permanent.
- [ ] AC-3: **Inline editing.** `<EditableField>` component: hover → pencil icon, click → contentEditable, blur or Enter → PATCH API call, Esc → cancel. Fields: SIM label, SIM notes, policy name, operator display_name, APN description, notification config name, segment name. Optimistic update with rollback on error.
- [ ] AC-4: **Export CSV on every list page.** "Export" button (icon + dropdown: CSV, Excel). Exports current filtered view. Filename includes page + active filters + date: `sims_active_operator-vodafone_2026-04-11.csv`. Backend: `GET /api/v1/{resource}/export?format=csv&{filters}` with streaming response. Frontend shows progress for large exports. Applied to: SIMs, APNs, operators, policies, sessions, jobs, audit, CDRs, notifications, violations, alerts, anomalies, users, API keys.
- [ ] AC-5: **Empty state CTAs.** Every list page with zero results shows: illustration/icon + title ("No SIMs yet") + description + primary CTA button ("Import your first SIMs" or "Create SIM"). First-time user checklist on Dashboard: "Welcome to Argus" → [ ] Connect an operator → [ ] Create an APN → [ ] Import SIMs → [ ] Create a policy. Each step links to relevant page.
- [ ] AC-6: **Data freshness indicator.** Footer bar on every data page: "Last updated 30s ago" (relative time) + manual "Refresh" button. Auto-refresh toggle (15s/30s/1m/off). For WS-fed pages (dashboard, sessions): "Live" green badge. For polled pages: stale indicator turns yellow after 5min.
- [ ] AC-7: **Sticky table headers + column customization.** All list tables: sticky header row on vertical scroll. Sticky first column (ID/name) on horizontal scroll. Column gear icon → panel: checkboxes for column visibility, drag-to-reorder, reset to default. Preferences persisted in `user_preferences` or localStorage.
- [ ] AC-8: **Form field-level validation.** Shared `useFormValidation` hook with rules per field (required, minLength, maxLength, pattern, custom). Validation runs `onChange` (debounced 300ms). Inline error message below field (red text). Required fields marked with red asterisk. Form submit blocked until all fields valid. Dirty state: navigate-away shows "Unsaved changes" confirmation dialog. Apply to: all create/edit forms (SIM, APN, operator, policy, user, API key, segment, notification config, roaming agreement).
- [ ] AC-9: **Admin impersonation.** `POST /api/v1/admin/impersonate/:user_id` (super_admin only) → issues JWT for that user. Purple banner across top of portal: "Viewing as [user email] — [Tenant name] | [Exit Impersonation]". Click Exit → returns to super_admin session. All actions during impersonation logged to audit with `impersonated_by` field. Can view but NOT modify data in impersonation mode (read-only mode enforced by backend middleware flag).
- [ ] AC-10: **Announcement banner.** `announcements` table (`id`, `title`, `body`, `type` (info/warning/critical), `target` (all/tenant_id), `starts_at`, `ends_at`, `dismissible`, `created_by`). Backend: `GET /announcements/active` returns current announcements. Frontend: colored banner below topbar (blue=info, yellow=warning, red=critical). Dismissible per-user (stored in localStorage). Admin CRUD page under System.
- [ ] AC-11: **Localization (TR/EN).** `react-i18next` integration. Language toggle in user settings and topbar. All UI strings in `web/src/locales/{en,tr}.json`. Date format: `DD.MM.YYYY` for TR, `MM/DD/YYYY` for EN. Number format: `1.234.567` for TR, `1,234,567` for EN. Relative time: "2 saat önce" vs "2 hours ago". User locale persisted in `users.locale` column (new migration if needed).
- [ ] AC-12: **Table density toggle.** Settings or toolbar button: "Comfortable" (default, 3rem rows) / "Compact" (2rem rows). CSS variable `--table-row-height` applied globally. Persisted in user preferences.
- [ ] AC-13: **Chart export + annotation.** Every time-series chart: "Export as PNG" button (html2canvas or chart library export). "Compare to previous period" toggle (overlay previous period as dashed line). Manual annotation: click chart → "Add marker" → label + timestamp → persisted in `chart_annotations` table → visible as vertical line with tooltip.
- [ ] AC-14: **Progress bars + optimistic updates.** Long operations (bulk actions, exports, report generation) show progress bar with percentage + ETA. Create/edit operations use optimistic update (cache updated immediately, rollback on error). Toast notifications for async results: "Bulk suspend started — 0/100" → "50/100" → "Complete — 100 SIMs suspended" (via WS progress events).
- [ ] AC-15: **Row click behavior clarified.** On every list: single click row → navigate to detail (primary action). Checkbox click → toggle select (no navigate). Right-click → context menu (same as ellipsis from STORY-076 AC-6). Double-click → quick-edit mode (inline edit from AC-3). Consistent across all lists.
- [ ] AC-16: **Comparison views extended.** Beyond existing SIM compare: Policy Compare (2 policies side-by-side, DSL diff + assignment count diff), Operator Compare (capabilities, health, cost, SIM count side-by-side). Accessible from list page: select 2-3 → "Compare" button in action bar.

## Dependencies
- Blocked by: STORY-075 (detail pages for inline edit targets), STORY-076 (row actions + keyboard shortcuts)
- Blocks: Phase 10 Gate (final UX quality bar)

## Test Scenarios
- [ ] E2E: Save "Active VF SIMs" view on SIM list → appears in sidebar → click → filters restored.
- [ ] E2E: Bulk suspend 10 SIMs → toast shows "10 SIMs suspended [Undo]" → click Undo within 10s → SIMs restored to previous state.
- [ ] E2E: Hover SIM label → pencil icon → click → edit → blur → label updated without page reload.
- [ ] E2E: SIM list → Export CSV → file downloads with filtered data, filename includes active filters.
- [ ] E2E: Empty SIM list (new tenant) → shows "Import your first SIMs" button → click → redirects to import page.
- [ ] E2E: Dashboard shows "Live" green badge. Stop WS server → badge turns "Offline" yellow.
- [ ] E2E: SIM list → scroll down 100 rows → header row still visible (sticky).
- [ ] E2E: Create APN form → leave name empty → blur → red "Required" error appears inline → submit button disabled.
- [ ] E2E: super_admin clicks "Impersonate" on user → purple banner appears → all pages show tenant data → click "Exit" → back to super_admin.
- [ ] E2E: Admin creates announcement "Maintenance Saturday 2am" → all users see blue banner → dismiss → banner gone → refresh → still dismissed.
- [ ] E2E: Switch language to TR → dates show DD.MM.YYYY, numbers show 1.234.567, UI labels in Turkish.

## Effort Estimate
- Size: L-XL
- Complexity: Medium-High (many independent UX features, each well-scoped)
