# STORY-047: Frontend Monitoring Pages (Sessions, Jobs, eSIM, Audit)

## User Story
As a platform user, I want monitoring pages for live sessions, jobs, eSIM profiles, and audit logs, so that I can track real-time activity and operations.

## Description
Live sessions page (SCR-050) with real-time table updated via WebSocket. Jobs page (SCR-080) with progress bars and status. eSIM profiles page (SCR-070) with profile management. Audit log page (SCR-090) with search, filter, and hash chain verification.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-02 (WebSocket), SVC-10 (Audit)
- API Endpoints: API-100, API-101, API-120 to API-123, API-070 to API-074, API-140 to API-142
- Source: docs/architecture/api/_index.md (Sessions, Jobs, eSIM, Audit sections)

## Screen Reference
- SCR-050: Live Sessions — real-time session table with duration, usage, operator, APN
- SCR-080: Job List — job table with type, status, progress bar, duration, actions
- SCR-070: eSIM Profiles — profile list with operator, state, enable/disable/switch actions
- SCR-090: Audit Log — searchable log table with action, user, entity, timestamp

## Acceptance Criteria
- [ ] Live sessions: table with SIM, operator, APN, NAS IP, duration, bytes in/out, IP address
- [ ] Live sessions: real-time updates via WebSocket session.started / session.ended
- [ ] Live sessions: new session appears at top with highlight animation
- [ ] Live sessions: ended session fades out or moves to "recently ended" section
- [ ] Live sessions: force disconnect button per session (confirmation required)
- [ ] Live sessions: stats bar (total active, by operator, avg duration)
- [ ] Jobs: table with type, state (badge), progress bar, total/processed/failed, duration, created by
- [ ] Jobs: filter by type, state
- [ ] Jobs: click row → detail panel with error report, retry/cancel actions
- [ ] Jobs: progress updates via WebSocket job.progress
- [ ] Jobs: job.completed event → state badge updates
- [ ] eSIM profiles: table with SIM ICCID, operator, profile state, actions (enable/disable/switch)
- [ ] eSIM profiles: filter by operator, state
- [ ] eSIM profiles: switch action → dialog to select target profile
- [ ] Audit log: searchable table with action, user, entity type, entity ID, timestamp, IP
- [ ] Audit log: filter by action type, user, entity type, date range
- [ ] Audit log: expandable row showing full detail (before/after JSON diff)
- [ ] Audit log: "Verify integrity" button → hash chain verification result

## Dependencies
- Blocked by: STORY-041 (scaffold), STORY-042 (auth), STORY-040 (WebSocket)
- Blocks: None

## Test Scenarios
- [ ] Live sessions loads current active sessions
- [ ] WebSocket session.started → new row appears with animation
- [ ] WebSocket session.ended → row removed/faded
- [ ] Force disconnect → confirmation → session terminated
- [ ] Jobs list shows progress bars for running jobs
- [ ] Job progress via WebSocket → progress bar advances
- [ ] Job detail panel shows error report for failed items
- [ ] eSIM profile enable → profile state updates to "enabled"
- [ ] eSIM profile switch → dialog → profile switched, table updates
- [ ] Audit log search → filtered results
- [ ] Audit log expand row → JSON diff shown
- [ ] Verify integrity → "Hash chain valid" / "Tamper detected" message

## Effort Estimate
- Size: XL
- Complexity: High
