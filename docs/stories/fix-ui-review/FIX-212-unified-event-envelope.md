# FIX-212: Unified Event Envelope + Name Resolution + Missing Publishers

## Problem Statement
NATS event payloads lack consistent structure. Each publisher defines its own shape. Event Stream UI, notification subscribers, and WS broadcasts re-parse differently. Additionally, some subjects are defined but have NO publisher (`sim.updated` — F-119), and some event fields contain raw UUIDs (F-14 pattern in events).

## User Story
As a platform engineer, I want every NATS event to follow a single canonical envelope so any subscriber — UI, notification, WS, audit — parses them uniformly.

## Architecture Reference
- NATS subject catalog: `internal/bus/nats.go`
- Envelope struct: shared type used by ALL publishers

## Findings Addressed
F-13, F-14, F-11, F-17, F-18, F-15, F-16, F-19, F-21, F-102, F-119 (sim.updated publisher), F-234 (Settings events drift), F-301 (publisher coverage matrix)

## Acceptance Criteria
- [ ] **AC-1:** Unified envelope:
  ```go
  type Envelope struct {
      ID        string                 `json:"id"`          // UUID
      Type      string                 `json:"type"`        // e.g. "session.started"
      Timestamp time.Time              `json:"timestamp"`
      TenantID  string                 `json:"tenant_id"`
      Severity  string                 `json:"severity"`    // critical|high|medium|low|info
      Title     string                 `json:"title"`       // human-readable
      Message   string                 `json:"message"`
      Entity    *EntityRef             `json:"entity,omitempty"`   // {type, id, display_name}
      Source    string                 `json:"source"`      // publisher component
      Meta      map[string]interface{} `json:"meta,omitempty"`
  }
  type EntityRef struct { Type, ID, DisplayName string }
  ```
- [ ] **AC-2:** All existing publishers (`gy.go`, `gx.go`, `radius/server.go`, `enforcer.go`, `s3_archival.go`, `backup.go`, `bulk_state_change.go`, etc.) updated to use envelope.
- [ ] **AC-3:** `sim.updated` publisher added — called by SIM handler on state/APN/operator/policy change (unblocks policy matcher — F-119).
- [ ] **AC-4:** Direct notification inserts (F-301 — `heartbeat_ok`, `user_login` direct `inApp.CreateNotification`) either (a) replaced with event-driven flow OR (b) kept as internal but NOT surfaced as "notifications" (F-217 cleanup).
- [ ] **AC-5:** Event catalog endpoint: `GET /api/v1/events/catalog` returns all event types + default severity + schema of `meta` field. Used by Notification Preferences (FIX-240) + Settings Notifications (F-234 canonical source).
- [ ] **AC-6:** Entity resolution — `EntityRef.display_name` filled by publisher (not subscriber). E.g., session.started payload has `entity: {type:"sim", id:UUID, display_name:"ICCID 89900100..."}`.
- [ ] **AC-7:** Doc `docs/architecture/WEBSOCKET_EVENTS.md` updated with envelope + all event types + example payloads.
- [ ] **AC-8:** Deprecation path — old-shape events still parseable by WS hub for 1 release (backward compat shim). Log warnings.

## Files to Touch
- `internal/bus/envelope.go` (NEW)
- All publishers (~20 files) — use envelope
- `internal/api/sim/handler.go` — `sim.updated` publish
- `internal/api/events/catalog_handler.go` (NEW)
- `internal/ws/hub.go` — parse envelope in relayNATSEvent
- `internal/notification/service.go` — handleAlert/handleHealthChanged consume envelope
- `docs/architecture/WEBSOCKET_EVENTS.md` — schema
- FE `web/src/types/events.ts` — TypeScript type matching envelope

## Risks & Regression
- **Risk 1 — Schema migration:** All publishers + subscribers change simultaneously. Mitigation: AC-8 backward shim for 1 release.
- **Risk 2 — Display name resolution cost:** publisher must lookup name → latency. Mitigation: cache common refs (operator, apn names).
- **Risk 3 — Event size bloat:** display_name + meta → larger payloads. NATS max message 1MB — acceptable.

## Test Plan
- Unit: envelope marshal/unmarshal roundtrip
- Integration: publish → all 4 subscribers parse correctly
- Browser: Live Event Stream shows entity names (not UUIDs)

## Plan Reference
Priority: P1 · Effort: XL · Wave: 3
