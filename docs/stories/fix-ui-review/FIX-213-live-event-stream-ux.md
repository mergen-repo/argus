# FIX-213: Live Event Stream UX — Filter Chips, Usage Display, Alert Body

## Problem Statement
Live Event Stream drawer shows raw event payloads with inconsistent formatting:
- Some events have no usage bytes displayed
- Alert events lack expanded body (severity + description)
- Filter dropdown absent — stream floods with low-value events (heartbeats)
- Timestamp relative only, no absolute time

## User Story
As an ops user, I want the Live Event Stream to be filterable, with clear event bodies and usage metrics, so I can triage real-time events without noise.

## Architecture Reference
- Consumes FIX-212 envelope
- FE: `web/src/components/live-event-stream/*`

## Findings Addressed
F-09, F-12, F-20, F-19

## Acceptance Criteria
- [ ] **AC-1:** Filter chips: type (multi-select), severity (5-level), entity (sim/operator/apn/policy), date range (current session / last 1h / last 24h).
- [ ] **AC-2:** Event card shows: severity badge, type chip, entity link (clickable), title, message, absolute timestamp + relative "3m ago".
- [ ] **AC-3:** Session events display usage: `bytes_in / bytes_out` (human-readable MB/GB) when `meta.bytes_in` present.
- [ ] **AC-4:** Alert events expanded: severity color, source, description, "Details" link to alert row in /alerts page.
- [ ] **AC-5:** Sticky filter header — chips stay while scrolling event list.
- [ ] **AC-6:** Pause/Resume button — stop auto-append while reviewing (button icon change + "N new events queued" badge on resume).
- [ ] **AC-7:** Clear button — resets list + filters.
- [ ] **AC-8:** Max 500 events in buffer; older events scrolled off. Memory bounded.
- [ ] **AC-9:** Virtual scrolling when buffer > 100 events (tanstack-virtual).

## Files to Touch
- `web/src/components/live-event-stream/stream.tsx`
- `web/src/components/live-event-stream/event-card.tsx`
- `web/src/components/live-event-stream/filter-bar.tsx` (NEW)
- `web/src/hooks/use-live-events.ts` — filter + pause state

## Risks & Regression
- **Risk 1 — Filter state not persisted:** User sets filter, closes drawer → reopens → lost. Mitigation: localStorage.
- **Risk 2 — WS backpressure under high event rate:** Virtual scrolling + max 500 buffer handles.

## Test Plan
- Unit: filter predicate logic
- Browser: filter chips toggle, pause/resume work, usage displays

## Plan Reference
Priority: P1 · Effort: M · Wave: 3
