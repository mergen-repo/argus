# STORY-044: Frontend SIM List & Detail

## User Story
As a SIM manager, I want a SIM list with segments, filters, virtual scrolling, and bulk actions, plus a detailed SIM view with all tabs, so that I can efficiently manage millions of SIMs.

## Description
SIM list (SCR-020) with segment dropdown, multi-field filter bar, data table with virtual scrolling for large datasets, bulk action toolbar, and cursor-based pagination. SIM detail (SCR-021 all tabs: overview, sessions, usage, diagnostics, history). Combo search across ICCID, IMSI, MSISDN. Group-first UX: segments as primary navigation, individual SIMs as drill-down.

## Architecture Reference
- Services: SVC-03 (Core API — SIM endpoints)
- API Endpoints: API-040 to API-053, API-060 to API-062
- Source: docs/architecture/api/_index.md (SIMs, SIM Segments sections)

## Screen Reference
- SCR-020: SIM List — segment dropdown, filter bar, data table, bulk actions, pagination
- SCR-021: SIM Detail — Overview tab (state, operator, APN, IP, policy, eSIM)
- SCR-021b: SIM Detail — Sessions tab (session history table)
- SCR-021c: SIM Detail — Usage tab (usage charts, CDR list)
- SCR-021d: SIM Detail — Diagnostics tab (diagnostic wizard, results)
- SCR-021e: SIM Detail — History tab (state transition timeline)

## Acceptance Criteria
- [ ] SIM list: data table with columns (ICCID, IMSI, MSISDN, Operator, APN, State, RAT, Usage 30d)
- [ ] Segment dropdown: select saved segment to filter list, show SIM count per segment
- [ ] Filter bar: quick filters for state, operator, APN, RAT type + free-text combo search
- [ ] Combo search: search by ICCID, IMSI, or MSISDN prefix (single input, auto-detect)
- [ ] Virtual scrolling: render only visible rows (react-virtuoso or similar)
- [ ] Cursor-based pagination: "Load more" or infinite scroll
- [ ] Bulk action toolbar: appears when SIMs selected (checkbox), actions: suspend, resume, terminate, assign policy
- [ ] Multi-select: individual checkboxes + "select all in segment" option
- [ ] SIM detail overview tab: state badge, operator, APN, IP address, assigned policy, eSIM profile (if applicable)
- [ ] SIM detail state actions: activate, suspend, resume, terminate, report lost (based on current state)
- [ ] SIM detail sessions tab: session history table with duration, usage, status
- [ ] SIM detail usage tab: usage chart (30-day trend), CDR table
- [ ] SIM detail diagnostics tab: run diagnostics button, step-by-step results display
- [ ] SIM detail history tab: state transition timeline with who/when/why
- [ ] Empty states: "No SIMs found" with create/import CTA

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-011 (SIM API), STORY-012 (segments API)
- Blocks: None

## Test Scenarios
- [ ] SIM list loads with first page of results
- [ ] Select segment → list filtered, count updated
- [ ] Search by ICCID prefix → matching SIMs shown
- [ ] Filter by state=active → only active SIMs shown
- [ ] Scroll down → next page loaded (cursor pagination)
- [ ] Select 3 SIMs → bulk action toolbar appears
- [ ] Bulk suspend → confirmation dialog → SIMs suspended
- [ ] Click SIM row → navigate to SIM detail
- [ ] SIM detail overview shows correct state, operator, APN
- [ ] Diagnostics tab → run diagnostics → step results displayed
- [ ] History tab → timeline of state transitions shown
- [ ] Empty segment → "No SIMs found" message

## Effort Estimate
- Size: XL
- Complexity: High
