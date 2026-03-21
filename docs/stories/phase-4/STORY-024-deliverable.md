# Deliverable: STORY-024 — Policy Dry-Run Simulation

## Summary

Implemented policy dry-run simulation that evaluates a policy version against the SIM fleet without applying changes. Returns affected SIM breakdown by operator, APN, and RAT type with behavioral change detection and sample SIMs showing before/after comparison.

## What Was Built

### Dry-Run Service (internal/policy/dryrun/service.go)
- `Execute()`: Full dry-run with SIM fleet evaluation, breakdown aggregation, behavioral change detection
- `CountMatchingSIMs()`: Count SIMs matching policy MATCH block filters
- `buildFiltersFromMatch()`: Extract query filters from compiled MATCH conditions
- `DetectBehavioralChanges()`: Compare before/after policy evaluation (QoS upgrade/downgrade, charging changes, access changes)
- Redis caching with 5-minute TTL, keyed by version_id + segment_id
- Sample SIMs (first 10) with full before/after policy result

### Async Job Processor (internal/job/dryrun.go)
- `DryRunProcessor` implementing `job.Processor` interface
- Handles large fleets (>100K SIMs) as background job
- Updates policy version with dry-run result on completion
- NATS event publication for job progress

### API Handler Extension (internal/api/policy/handler.go)
- `DryRun()` handler: POST /api/v1/policy-versions/:id/dry-run
- Sync response (200) for <100K SIMs, async (202 with job_id) for >100K
- Optional segment_id filter
- DSL validation before execution (422 on invalid)

### Store Extensions
- `sim.go`: SIMFleetFilters, CountByFilters, AggregateByOperator/APN/RATType, FetchSample
- `policy.go`: UpdateDryRunResult, GetVersionWithTenant (tenant-scoped via JOIN)
- `job.go`: CreateWithTenantID for explicit tenant job creation
- `nats.go`: PublishRaw for pre-serialized messages

### Tests
- 22 service tests + 4 handler tests = 26 new tests
- Full suite: 623 tests passing, zero regressions

## Architecture References Fulfilled
- API-094: POST /api/v1/policy-versions/:id/dry-run
- SVC-05: Policy evaluation via DSL evaluator
- SVC-09: Background job for large fleet processing

## Files Changed
```
internal/policy/dryrun/service.go      (new)
internal/policy/dryrun/service_test.go (new)
internal/job/dryrun.go                 (new)
internal/store/sim.go                  (modified)
internal/store/policy.go               (modified)
internal/store/job.go                  (modified)
internal/bus/nats.go                   (modified)
internal/api/policy/handler.go         (modified)
internal/api/policy/handler_test.go    (modified)
internal/gateway/router.go             (modified)
cmd/argus/main.go                      (modified)
```

## Dependencies Unblocked
- STORY-025 (Policy Staged Rollout) — can use dry-run data for stage planning
