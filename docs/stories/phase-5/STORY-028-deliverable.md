# Deliverable: STORY-028 — eSIM Profile Management

## Summary

Implemented eSIM profile CRUD with lifecycle management. Profile state machine: available → enabled ↔ disabled → deleted. Atomic profile switch operation (disable old + enable new + update SIM in single transaction). SM-DP+ adapter interface with mock implementation for development.

## What Was Built

### Store (internal/store/esim.go)
- List (cursor pagination, filters: sim_id, operator_id, state), GetByID
- Enable (available/disabled → enabled, one-per-SIM enforcement)
- Disable (enabled → disabled)
- Switch (atomic: disable old + enable new + update SIM operator/apn in TX)
- GetEnabledProfileForSIM
- Tenant scoping via JOIN sims (esim_profiles has no tenant_id column)
- FOR UPDATE row locks for state changes

### SM-DP+ Adapter (internal/esim/smdp.go)
- SMDPAdapter interface: DownloadProfile, EnableProfile, DisableProfile, DeleteProfile
- MockSMDPAdapter: simulates profile operations with logging

### API Handlers (internal/api/esim/handler.go)
- API-070: GET /api/v1/esim-profiles (list with filters)
- API-071: GET /api/v1/esim-profiles/:id (detail)
- API-072: POST /api/v1/esim-profiles/:id/enable
- API-073: POST /api/v1/esim-profiles/:id/disable
- API-074: POST /api/v1/esim-profiles/:id/switch
- Standard envelope responses, audit logging

### Error Codes (internal/apierr/apierr.go)
- PROFILE_ALREADY_ENABLED, NOT_ESIM, INVALID_PROFILE_STATE, SAME_PROFILE, DIFFERENT_SIM

### Tests
- 6 store tests + 5 handler tests = 11 new tests
- Full suite: 1100 tests passing, zero regressions

## Files Changed
```
internal/store/esim.go           (new)
internal/store/esim_test.go      (new)
internal/esim/smdp.go            (new)
internal/api/esim/handler.go     (new)
internal/api/esim/handler_test.go (new)
internal/apierr/apierr.go        (modified)
internal/gateway/router.go       (modified)
cmd/argus/main.go                (modified)
```

## Dependencies Unblocked
- STORY-030 (Bulk Operations) — can use eSIM profile switch for bulk operator changes
