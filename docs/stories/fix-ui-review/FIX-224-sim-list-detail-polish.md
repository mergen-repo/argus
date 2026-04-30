# FIX-224: SIM List/Detail Polish — State Filter, Created Datetime, Bulk Bar Sticky, Compare Limit, Import Validation

## Problem Statement
SIM list/detail has multiple polish items:
- State filter single-select (need multi-select)
- Created column date only, no time
- Bulk action bar non-sticky (F-106 covered by FIX-201)
- Compare feature allows >4 SIMs (unusable)
- Import CSV validation sparse (errors only surfaced after import)

## User Story
As a SIM operator, I want refined list/detail controls for ergonomic daily use.

## Findings Addressed
F-85, F-86, F-87, F-91, F-92

## Acceptance Criteria
- [ ] **AC-1:** State filter multi-select: active, suspended, terminated, stolen_lost, pending (checkbox dropdown).
- [ ] **AC-2:** Created column: "4/19/2026 15:59" + relative "2d ago" tooltip.
- [ ] **AC-3:** Bulk action bar sticky (FIX-201 AC-10).
- [ ] **AC-4:** Compare feature limited to 4 SIMs max; 5th selection replaces oldest OR shows warning.
- [ ] **AC-5:** Import CSV: pre-upload validation — column check (ICCID, IMSI, MSISDN required), format validate, row count, estimated time. Show preview table before commit.
- [ ] **AC-6:** Import post-process report: N succeeded, M failed with reasons (duplicate ICCID, invalid operator_id, etc.), export CSV of failed rows.

## Files to Touch
- `web/src/pages/sims/index.tsx`
- `web/src/pages/sims/compare.tsx`
- `web/src/pages/sims/import.tsx` or Dialog

## Risks & Regression
- Low risk polish.

## Test Plan
- Browser: multi-state filter, compare 5 shows limit, import preview

## Plan Reference
Priority: P2 · Effort: M · Wave: 6
