# FIX-227: APN Connected SIMs SlidePanel — CDR + Usage Graph + Quick Stats

## Problem Statement
APN Detail page "Connected SIMs" tab shows list but lacks quick actions / stats. Users want inline peek without navigating to SIM detail.

## User Story
As an APN owner, I want to click a SIM row in the APN's Connected SIMs list and see a side panel with CDR summary, usage graph, quick stats, without leaving the APN context.

## Findings Addressed
F-72

## Acceptance Criteria
- [ ] **AC-1:** SIM row click → SlidePanel (FIX-216 pattern) with: ICCID/IMSI/MSISDN header, current state badge, policy applied, last session.
- [ ] **AC-2:** Panel body: usage sparkline (last 7d bytes_in/out), CDR summary (total sessions, total bytes, avg duration), top destinations (future).
- [ ] **AC-3:** Quick actions: "View Full Details" (navigate to /sims/{id}), "Suspend", "View CDRs" (navigate to /cdrs?sim_id=X — FIX-214 link).
- [ ] **AC-4:** Panel resource efficiency — data loaded on open only (lazy fetch).

## Files to Touch
- `web/src/pages/apns/detail.tsx` — SIMs tab → SlidePanel
- `web/src/components/sims/quick-view-panel.tsx` (NEW)

## Risks & Regression
- Low risk — additive.

## Test Plan
- Browser: click SIM in APN detail → panel opens with data

## Plan Reference
Priority: P2 · Effort: S · Wave: 6 · Depends: FIX-216 (modal pattern), FIX-214 (CDR link)
