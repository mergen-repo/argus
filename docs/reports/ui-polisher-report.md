# UI Polisher Report

**Date:** 2026-03-23
**Inspector:** Claude Opus 4.6 (UI Polisher Agent)
**Status:** PASS

## Design Token Compliance

### Token Source
- `docs/FRONTEND.md` -- design system specification
- `web/src/index.css` -- Tailwind v4 `@theme` block with all custom tokens

### Automated Scan Results

| Check | Result |
|-------|--------|
| Hardcoded hex colors in `.tsx` | **0 violations** -- no hardcoded hex colors in component files |
| Default Tailwind colors (gray, slate, zinc, etc.) | **0 violations** -- all colors use semantic tokens |
| Arbitrary pixel values `[Npx]` | **206 occurrences** -- all align with FRONTEND.md spec (10px labels, 11px secondary, 16px headings, 28px metrics, etc.) |
| `bg-white` / `text-white` usage | **4 occurrences** -- 3 legitimate (contrast on colored bg), 1 fixed |
| `bg-black` usage | **3 occurrences** -- all in overlay backdrops (dialog, sheet, command palette) -- correct |
| `rounded-[var(--radius-*)]` usage | **Consistent** -- all components use design tokens for border-radius |
| `rounded-[4px]` / `rounded-[3px]` | **3 occurrences** -- inner elements (tabs, dropdown items, checkboxes) -- intentionally smaller than `--radius-sm` |

### Token Summary
The codebase is **highly compliant** with the design system. All colors, spacing, typography, shadows, and radii use the semantic tokens defined in `index.css`. No violations of the core design token system were found.

## Visual Inspection

### Screens Inspected (23 total)

| # | Screen | Route | Status |
|---|--------|-------|--------|
| 1 | Login | `/login` | Pass -- dark bg, proper card, neon accent |
| 2 | Dashboard | `/` | Pass (1 fix applied) -- metric cards, charts, alert feed |
| 3 | SIM List | `/sims` | Pass -- table, filters, pagination, checkboxes |
| 4 | SIM Detail | `/sims/:id` | Pass -- tabs, info cards, action buttons |
| 5 | APN List | `/apns` | Pass -- card grid, IP pool bars, badges |
| 6 | APN Detail | `/apns/:id` | Pass -- tabs, config display |
| 7 | Operators | `/operators` | Pass -- card grid, health badges, RAT badges |
| 8 | Sessions | `/sessions` | Pass -- live indicator, metric cards, table |
| 9 | Policies | `/policies` | Pass -- table, scope badges, status |
| 10 | Jobs | `/jobs` | Pass -- progress bars, state badges |
| 11 | Audit Log | `/audit` | Pass -- empty state with icon+message |
| 12 | Notifications | `/notifications` | Pass -- unread/read tabs, severity icons |
| 13 | Analytics Usage | `/analytics` | Pass (1 fix applied) -- filters, charts, breakdowns |
| 14 | Analytics Cost | `/analytics/cost` | Pass -- cost cards, carrier comparison |
| 15 | Analytics Anomalies | `/analytics/anomalies` | Pass -- severity badges, state actions |
| 16 | Users & Roles | `/settings/users` | Pass -- table, role buttons, status badges |
| 17 | API Keys | `/settings/api-keys` | Pass (1 fix applied) -- scope badges, actions |
| 18 | IP Pools | `/settings/ip-pools` | Pass -- utilization bars, color-coded |
| 19 | Notification Config | `/settings/notifications` | Pass (1 fix applied) -- toggle switches, sliders |
| 20 | System Health | `/system/health` | Pass -- gauge charts, service status cards |
| 21 | Tenants | `/system/tenants` | Pass -- table, plan badges |
| 22 | eSIM | `/esim` | Pass -- empty state with icon+message |
| 23 | 404 Page | `/nonexistent` | Pass -- neon 404, Go Back + Dashboard buttons |

### Key Visual Checks

| Category | Status |
|----------|--------|
| Dark mode consistency | **Pass** -- all pages use `--bg-primary` (#06060B), no white flashes |
| Card/panel consistency | **Pass** -- all cards use `--radius-md`, `--shadow-card`, consistent padding |
| Table consistency | **Pass** -- all tables use `text-[13px]`, consistent header style, hover states |
| Button consistency | **Pass** -- all buttons use `buttonVariants` with proper sizing/colors |
| Empty states | **Pass** -- present on Audit Log, eSIM, and data tables (icon + heading + description) |
| Error handling | **Pass** -- ErrorBoundary component exists with proper styling |
| 404 page | **Pass** -- styled with neon accent, descriptive message, navigation buttons |
| Sidebar active state | **Pass** -- correctly highlights current route on all pages |
| Glass-morphism header | **Pass** -- header uses backdrop-blur and semi-transparent bg |
| Sparklines on dashboard | **Pass** -- metric cards show sparkline bars |
| Ambient background | **Pass** -- subtle radial gradients visible on all pages |

## Issues Found & Fixed

### Fix 1: Dashboard SIM Distribution -- "Stolen_lost" label formatting
- **File:** `web/src/pages/dashboard/index.tsx`
- **Issue:** `stolen_lost` state displayed as "Stolen_lost" in pie chart legend
- **Fix:** Split on underscore, capitalize each word, join with `/` -- now shows "Stolen/Lost"
- **Also:** Added `stolen_lost` to `STATE_COLORS` map so it gets purple color instead of falling back to gray

### Fix 2: Toggle switch knob using `bg-white` instead of token
- **File:** `web/src/pages/settings/notifications.tsx`
- **Issue:** Toggle switch knob used hardcoded `bg-white` instead of semantic token
- **Fix:** Changed to `bg-text-primary` which matches the design system

### Fix 3: API Keys rate limit showing "/min" without value
- **File:** `web/src/pages/settings/api-keys.tsx`
- **Issue:** When `rate_limit` is 0/falsy, column showed "/min" with no number
- **Fix:** Added conditional: shows `{rate_limit}/min` when truthy, `-` otherwise

### Fix 4: Analytics breakdown dimension title and key formatting
- **File:** `web/src/pages/dashboard/analytics.tsx`
- **Issue:** `rat_type` dimension displayed as "Rat_type Breakdown" -- underscore visible
- **Fix:** Replace underscores with spaces before capitalizing -- now shows "Rat type Breakdown"
- **Also:** Breakdown keys now show full text when under 12 chars (helps rat_type items like `nb_iot`, `lte_m`) and added `title` attribute for full key on hover

## Observations (Not Fixed -- Require Backend Changes)

1. **Truncated UUIDs in data displays:** Sessions table, analytics breakdowns, and dashboard "Top 5 APNs by Traffic" chart show truncated UUIDs (e.g., "20000000", "06000000...") instead of human-readable names. This requires the backend API to return resolved names alongside IDs.

2. **WebSocket connection errors:** Console shows repeated `WebSocket connection to 'ws://localhost:80...'` errors. The WebSocket server (port 8081) is configured but not responding to browser connections via the nginx proxy.

## TypeScript Verification

```
$ cd web && npx tsc --noEmit
(no errors)
```

All fixes compile cleanly.

## Summary

| Metric | Count |
|--------|-------|
| Screens inspected | 23 |
| Token violations found | 1 (bg-white on toggle knob) |
| Token violations fixed | 1 |
| Visual issues found | 4 |
| Visual issues fixed | 4 |
| Backend-dependent issues noted | 2 |
