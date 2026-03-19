# STORY-050: Frontend Onboarding Wizard & Notification Center

## User Story
As a new tenant admin, I want a guided onboarding wizard to set up my tenant, and a notification center for ongoing alerts, so that I can get started quickly and stay informed.

## Description
Onboarding wizard (SCR-003) — 5-step flow: (1) tenant profile, (2) operator connection, (3) APN configuration, (4) SIM import, (5) policy setup. Notification center drawer (SCR-100) slides in from right with unread/all tabs, mark as read, and click-to-navigate. Dark/light mode toggle in user menu.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-08 (Notification Service)
- API Endpoints: API-013 (tenant update), API-024 (operator test), API-031 (APN create), API-063 (SIM import), API-091 (policy create), API-130 to API-132 (notifications)
- Source: docs/architecture/api/_index.md

## Screen Reference
- SCR-003: Onboarding Wizard — 5-step stepper with back/next/skip, progress indicator
- SCR-100: Notifications — drawer panel with notification list, unread badge, mark as read

## Acceptance Criteria
- [ ] Onboarding wizard triggered on first login for new tenant admin (onboarding_completed=false)
- [ ] Step 1 — Tenant Profile: company name, logo upload, timezone, retention days
- [ ] Step 2 — Operator Connection: select operator, enter credentials, test connection (API-024)
- [ ] Step 3 — APN Configuration: create first APN with IP pool
- [ ] Step 4 — SIM Import: CSV upload or manual entry of first SIMs (API-063)
- [ ] Step 5 — Policy Setup: create first policy using template or blank DSL
- [ ] Progress indicator: step numbers with completion checkmarks
- [ ] Skip button on non-mandatory steps (steps 4, 5)
- [ ] "Complete Setup" on final step → set onboarding_completed=true, redirect to dashboard
- [ ] Wizard accessible again from settings if user wants to revisit
- [ ] Notification center: drawer slides from right edge
- [ ] Notification center: triggered by bell icon in topbar with unread count badge
- [ ] Notification center: tabs (Unread / All)
- [ ] Notification center: each item shows type icon, title, message preview, timestamp
- [ ] Notification center: click item → navigate to relevant page (e.g., operator detail for operator.down)
- [ ] Notification center: mark single as read (swipe or button)
- [ ] Notification center: "Mark all as read" button
- [ ] Notification center: real-time via WebSocket notification.new event → badge increments
- [ ] Dark/light mode: toggle in user dropdown menu
- [ ] Dark/light mode: persisted in Zustand + localStorage
- [ ] Dark/light mode: Tailwind class strategy (dark: prefix)

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-038 (notification API), STORY-040 (WebSocket)
- Blocks: None

## Test Scenarios
- [ ] First login as new tenant → wizard appears
- [ ] Step 1: fill tenant info → next → Step 2
- [ ] Step 2: test connection success → green check, next enabled
- [ ] Step 2: test connection failure → error, retry button
- [ ] Skip step 4 → proceed to step 5
- [ ] Complete setup → redirected to dashboard, wizard doesn't show on next login
- [ ] Notification bell: shows unread count (e.g., "3")
- [ ] Click bell → drawer opens with notification list
- [ ] WebSocket notification.new → badge count increments, new item in drawer
- [ ] Click notification → drawer closes, navigate to relevant page
- [ ] Mark all as read → badge disappears, all items marked
- [ ] Toggle dark mode → theme switches, persisted across reload

## Effort Estimate
- Size: L
- Complexity: Medium
