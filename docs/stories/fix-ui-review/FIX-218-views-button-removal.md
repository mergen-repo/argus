# FIX-218: Views Button Global Removal + Checkbox Cleanup (Operators)

## Problem Statement
"Views" button exists on multiple list pages (Operators, APNs, etc.) with unclear purpose. User observed: removes space, adds noise. Operators page has checkbox column used for no bulk action.

## User Story
As a user, I want only interactive UI elements that perform actions — unused widgets removed.

## Findings Addressed
F-59, F-60, F-66, F-71, F-90

## Acceptance Criteria
- [ ] **AC-1:** Remove "Views" button from: Operators, APNs, IP Pools, SIMs, Policies, Sessions list pages.
- [ ] **AC-2:** Operators list checkbox column removed — no bulk operator action planned.
- [ ] **AC-3:** If future "saved views" feature needed, re-introduce with full implementation (not cosmetic).

## Files to Touch
- Each list page component in `web/src/pages/*/index.tsx`

## Risks & Regression
- **Risk 1 — User muscle memory:** Unused widget removal rarely breaks anyone.

## Test Plan
- Browser smoke test on each list page — no broken layout after removal

## Plan Reference
Priority: P2 · Effort: S · Wave: 5
