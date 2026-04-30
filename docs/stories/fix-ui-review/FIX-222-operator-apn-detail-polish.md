# FIX-222: Operator/APN Detail Polish — KPI Row, Tab Consolidation, Tooltips, Missing eSIM Tab

## Problem Statement
Operator Detail (11 tabs) and APN Detail (8 tabs) have quality issues:
- KPI row has empty/stale cells (F-52)
- Too many tabs with overlap (Circuit Breaker + Health, Notifications + Alerts) — hard to scan
- Technical terms (MCC, MNC, EID, MSISDN, APN) not explained — new admins lost
- Operator Detail lacks "eSIM Profiles" tab (F-184 — reverse link from SIMs → eSIM missing)
- SIMs tab uses client-side filter against operator; needs server-side (ties into F-149/FIX-233)
- Protocols tab "No probe run yet" stuck when backend health check passing

## User Story
As an ops engineer, I want operator and APN detail pages to show relevant KPIs at top, clearly labeled tabs grouped by concern (overview/operations/data), and tooltips on technical terms — so I can quickly scan an entity's state.

## Architecture Reference
- FE: `web/src/pages/operators/detail.tsx`, `apns/detail.tsx`
- New shared: InfoTooltip component

## Findings Addressed
- F-52 (KPI row empty cells)
- F-53/F-54 (operator tab overlap)
- F-56/F-57 (Protocols "no probe" stale)
- F-58 (term tooltips)
- F-68/F-69/F-70 (APN detail gaps)
- F-88 (tab consistency)
- F-184 (eSIM tab missing on operator)
- F-185 (Test Connection probe results not shown)

## Acceptance Criteria

### Operator Detail
- [ ] **AC-1:** **KPI row** — 4 metrics top-row, data-driven, no empty cells:
  - SIMs (total, with chip for state breakdown on hover)
  - Active Sessions (current count + trend arrow vs 1h ago)
  - Auth/s (last 1h rolling, via `/operators/{id}/metrics`)
  - Health Uptime % (last 24h, from `health_history`)
- [ ] **AC-2:** **Tab consolidation** — from 11 to 8:
  - **Overview** (current)
  - **Protocols** (current) — config + live test
  - **Health** (merges current Health + Circuit Breaker) — history timeline + breaker state + trip log
  - **Traffic** (current)
  - **Sessions** (current)
  - **SIMs** (current, fix server-side filter via FIX-149/FIX-233)
  - **eSIM Profiles** [NEW — F-184] — profile stock summary + linked list
  - **Alerts** (merges current Alerts + Notifications — all entity-scoped messages)
  - **Audit** (current)
  - Removed from UI: Agreements (FIX-238 roaming removal), Notifications standalone (merged)
- [ ] **AC-3:** **Protocols tab polish:**
  - Each protocol card shows last probe result (latency + success) IF available — not stuck "No probe run yet"
  - Live Test Connection button — fires `POST /operators/{id}/test/{protocol}` → result inline for 60s
  - Auto-probe checkbox (future — disabled placeholder)
  - Circuit breaker state inline (Closed / Half-Open / Open with last-trip time)
- [ ] **AC-4:** **Health tab (post-merge):**
  - Timeline chart (last 24h/7d/30d toggle)
  - Circuit breaker history (open/close events)
  - Per-protocol latency trends sparkline
  - SLA target line on chart (FIX-215 integration)

### APN Detail
- [ ] **AC-5:** **KPI row** — 4 metrics:
  - SIMs (count)
  - Active Sessions (current)
  - Traffic Last 24h (bytes with humanize)
  - Top Operator using this APN
- [ ] **AC-6:** **Tab order** (8 tabs grouped):
  - Overview → Config → IP Pools → SIMs → Traffic (data)
  - Policies (references) → Audit → Alerts/Notifications (events)
- [ ] **AC-7:** APN SIMs tab uses server-side filter (no client-side — matches FIX-202 / FIX-236 pattern).

### Technical term tooltips
- [ ] **AC-8:** New `InfoTooltip` component. Applied to labels:
  - **MCC** — "Mobile Country Code (3 digits identifying country, e.g. 286 = Turkey)"
  - **MNC** — "Mobile Network Code (2-3 digits identifying operator within country)"
  - **EID** — "Embedded UICC Identifier (32-digit eUICC chip serial)"
  - **MSISDN** — "Mobile Station ISDN Number (phone number)"
  - **APN** — "Access Point Name (network entry identifier)"
  - **IMSI** — "International Mobile Subscriber Identity (15-digit subscriber ID)"
  - **ICCID** — "Integrated Circuit Card Identifier (SIM card serial)"
  - **CoA** — "Change of Authorization (mid-session policy update, RFC 5176)"
  - **SLA** — "Service Level Agreement (uptime contract)"
- [ ] **AC-9:** Tooltips appear on hover (desktop) + tap (mobile) of info ⓘ icon next to term. Delay 500ms. ESC closes.

### Consistency
- [ ] **AC-10:** Tab order consistent across both pages: **read-heavy first** (Overview, KPI), **operations second** (Config, Actions), **data dumps last** (Audit).
- [ ] **AC-11:** Tab state persisted in URL: `?tab=traffic` deep-linkable. Old removed tabs redirect to Overview.
- [ ] **AC-12:** Action buttons (Edit, Delete, Archive) consistent top-right on both pages. No in-tab overload.

### Data integrity
- [ ] **AC-13:** Operator Detail "SIMs" tab count matches List page count (FIX-208 aggregation source). If tenant-scoped (F-225), clearly label — "All SIMs in this tenant: N" vs "Cross-tenant: N (super_admin)"

## Files to Touch
- `web/src/pages/operators/detail.tsx`
- `web/src/pages/apns/detail.tsx`
- `web/src/components/ui/info-tooltip.tsx` (NEW or enhance existing)
- `web/src/components/operator/esim-profiles-tab.tsx` (NEW — for FIX-184)
- `web/src/hooks/use-operator-detail.ts` — metrics endpoint
- `docs/GLOSSARY.md` — tooltip copy consolidated here

## Risks & Regression
- **Risk 1 — Tab removal breaks direct links:** AC-11 redirect old tab params.
- **Risk 2 — KPI metric queries heavy:** Use already-cached endpoints (FIX-208 Aggregates service).
- **Risk 3 — Tooltip content drift:** AC-9 single source in GLOSSARY.md — pull at build time.
- **Risk 4 — Circuit breaker merge loses detail:** AC-4 ensures breaker section dedicated within Health tab, not hidden.

## Test Plan
- Browser: each detail page — KPI filled, tab consolidation clear, tooltips appear on hover
- Accessibility: keyboard nav, screen reader announces tooltips
- URL deep-link: `?tab=health` works post-merge

## Plan Reference
Priority: P2 · Effort: M · Wave: 6 · Depends: FIX-202 (DTO), FIX-208 (aggregates), FIX-235 (eSIM subsystem for operator tab)
