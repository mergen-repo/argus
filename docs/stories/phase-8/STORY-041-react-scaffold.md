# STORY-041: React + Vite + Tailwind + shadcn/ui Scaffold

## User Story
As a frontend developer, I want the React project scaffold with routing, state management, API client, and design system ready, so that all subsequent frontend stories have a working foundation.

## Description
Set up React 18+ with Vite, Tailwind CSS, shadcn/ui component library, React Router with all routes from SCREENS.md, DashboardLayout + AuthLayout, Zustand stores (auth, UI preferences), TanStack Query for API data fetching, WebSocket client for real-time events, and command palette (Ctrl+K). FRONTEND.md design tokens applied as Tailwind theme. Dark-first design, data-dense layouts.

## Architecture Reference
- Services: SVC-01 (API Gateway — frontend static serving via Nginx)
- Packages: web/ directory (React SPA)
- Source: docs/SCREENS.md (all routes), docs/FRONTEND.md (design tokens)

## Screen Reference
- All screens: routing setup for SCR-001 through SCR-121
- DashboardLayout: sidebar nav, topbar, command palette
- AuthLayout: centered card layout for login/2FA/onboarding

## Acceptance Criteria
- [ ] Vite + React 18 + TypeScript project in web/ directory
- [ ] Tailwind CSS configured with FRONTEND.md design tokens (colors, spacing, typography)
- [ ] shadcn/ui installed with base components: Button, Input, Card, Table, Dialog, Select, Tabs, DropdownMenu, Badge, Avatar, Tooltip, Sheet, Command
- [ ] Dark mode as default, light mode toggle ready
- [ ] React Router v6 with all routes from SCREENS.md:
  - /login, /login/2fa, /setup (AuthLayout)
  - /, /analytics, /analytics/cost, /analytics/anomalies (DashboardLayout)
  - /sims, /sims/:id (DashboardLayout)
  - /apns, /apns/:id, /operators, /operators/:id (DashboardLayout)
  - /sessions, /policies, /policies/:id (DashboardLayout)
  - /esim, /jobs, /audit, /notifications (DashboardLayout)
  - /settings/users, /settings/api-keys, /settings/ip-pools, /settings/notifications (DashboardLayout)
  - /system/health, /system/tenants (DashboardLayout)
- [ ] DashboardLayout: collapsible sidebar with nav groups, topbar with search + notifications + user menu
- [ ] AuthLayout: centered card with Argus logo
- [ ] Zustand store — auth: user, token, permissions, login/logout actions
- [ ] Zustand store — ui: sidebar collapsed, dark mode, locale
- [ ] TanStack Query: configured with base URL, JWT interceptor, error handling
- [ ] API client: Axios instance with JWT header, refresh token interceptor, error toast
- [ ] WebSocket client: connect on auth, reconnect on disconnect, event dispatcher
- [ ] Command palette: Ctrl+K opens search dialog (search pages, SIMs, commands)
- [ ] Placeholder pages for all routes (component name + route displayed)
- [ ] `npm run dev` starts dev server with HMR
- [ ] `npm run build` produces optimized production bundle

## Dependencies
- Blocked by: STORY-001 (Nginx serves static files)
- Blocks: All other STORY-042 to STORY-050 (frontend stories)

## Test Scenarios
- [ ] `npm run dev` starts without errors, HMR works
- [ ] All routes render their placeholder pages
- [ ] DashboardLayout renders sidebar + topbar
- [ ] AuthLayout renders centered card
- [ ] Dark mode applied by default, toggle switches to light
- [ ] Ctrl+K opens command palette
- [ ] WebSocket client connects when auth token present
- [ ] TanStack Query fetches from /api/v1/* with JWT header
- [ ] `npm run build` produces bundle under 500KB gzipped (initial load)

## Effort Estimate
- Size: L
- Complexity: Medium
