# STORY-009: Operator CRUD & Health Check

## User Story
As a super admin, I want to manage operator connections and monitor their health status.

## Description
CRUD for operators (system-level, super_admin only), operator grants (tenant access), health check heartbeat, and circuit breaker state tracking. Includes mock simulator adapter.

## Architecture Reference
- Services: SVC-03 (CRUD), SVC-06 (Operator Router — health check)
- API Endpoints: API-020 to API-027
- Database Tables: TBL-05 (operators), TBL-06 (operator_grants), TBL-23 (operator_health_logs)
- Source: docs/architecture/db/operator.md

## Screen Reference
- SCR-040: Operator List (docs/screens/SCR-040-operator-list.md)
- SCR-041: Operator Detail (docs/screens/SCR-041-operator-detail.md)

## Acceptance Criteria
- [ ] POST /api/v1/operators creates operator with adapter config (encrypted in DB)
- [ ] GET /api/v1/operators lists all operators with health status
- [ ] PATCH /api/v1/operators/:id updates config, failover policy, circuit breaker thresholds
- [ ] POST /api/v1/operators/:id/test sends test request via adapter, returns latency + status
- [ ] POST /api/v1/operator-grants grants tenant access to operator
- [ ] Health check runs every health_check_interval_sec per operator
- [ ] Health status persisted to TBL-23 (TimescaleDB) and cached in Redis
- [ ] Circuit breaker state tracked: closed → open → half_open → closed
- [ ] Mock simulator adapter responds to health checks with configurable latency
- [ ] Operator adapter_config fields encrypted at rest (AES-256)

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-020 | GET | /api/v1/operators | — | `[{id,name,code,mcc,mnc,health_status,sim_count,adapter_type}]` | JWT(super_admin) | 200 |
| API-021 | POST | /api/v1/operators | `{name,code,mcc,mnc,adapter_type,adapter_config,supported_rat_types,failover_policy,...}` | `{id,name,...}` | JWT(super_admin) | 201, 400, 409 |
| API-023 | GET | /api/v1/operators/:id/health | — | `{status,latency_ms,circuit_state,last_check,uptime_24h,failure_count}` | JWT(op_manager+) | 200 |
| API-024 | POST | /api/v1/operators/:id/test | — | `{success:bool,latency_ms,error?}` | JWT(super_admin) | 200 |
| API-026 | POST | /api/v1/operator-grants | `{tenant_id,operator_id}` | `{id,tenant_id,operator_id,enabled}` | JWT(super_admin) | 201, 409 |

## Dependencies
- Blocked by: STORY-005 (tenant management)
- Blocks: STORY-010 (APN needs operators), STORY-011 (SIM needs operators)

## Test Scenarios
- [ ] Create operator with mock adapter → health check succeeds
- [ ] Grant operator to tenant → tenant can see operator in their list
- [ ] Health check failure increments circuit breaker counter
- [ ] 5 consecutive failures → circuit opens, health_status = "down"
- [ ] Test connection with mock adapter → returns latency
- [ ] Duplicate operator code → 409

## Effort Estimate
- Size: L
- Complexity: High
