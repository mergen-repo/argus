# STORY-010: APN CRUD & IP Pool Management

## User Story
As a tenant admin, I want to define APNs with IP address pools, so that SIMs can be organized by access point.

## Description
Full APN lifecycle (create, read, update, archive), IP pool CRUD with dual-stack (IPv4+IPv6), utilization monitoring, and address allocation/reclaim logic.

## Architecture Reference
- Services: SVC-03 (Core API)
- API Endpoints: API-030 to API-035 (APNs), API-080 to API-085 (IP Pools)
- Database Tables: TBL-07 (apns), TBL-08 (ip_pools), TBL-09 (ip_addresses)
- Source: docs/architecture/db/sim-apn.md

## Screen Reference
- SCR-030: APN List (docs/screens/SCR-030-apn-list.md)
- SCR-032: APN Detail (docs/screens/SCR-032-apn-detail.md)
- SCR-112: IP Pools (docs/screens/SCR-112-settings-ippools.md)

## Acceptance Criteria
- [ ] POST /api/v1/apns creates APN linked to operator + tenant
- [ ] APN name unique per (tenant_id, operator_id)
- [ ] DELETE /api/v1/apns/:id soft-deletes to ARCHIVED if no active SIMs, else 422
- [ ] POST /api/v1/ip-pools creates pool with CIDR range, auto-generates IP addresses
- [ ] IP allocation: next available from pool, conflict detection (no duplicate IPs)
- [ ] Static IP reservation per SIM via API-085
- [ ] Pool utilization alerts at configurable thresholds (80%, 90%, 100%)
- [ ] Pool utilization percentage updated on each allocate/release
- [ ] IPv4 + IPv6 dual-stack support (separate pools per family)
- [ ] IP reclaim: terminated SIM IPs marked "reclaiming" with grace period

## API Contract

| Ref | Method | Path | Request | Response (data) | Auth | Status Codes |
|-----|--------|------|---------|-----------------|------|-------------|
| API-031 | POST | /api/v1/apns | `{name,operator_id,apn_type,supported_rat_types,default_policy_id?}` | `{id,name,operator_id,...}` | JWT(tenant_admin+) | 201, 400, 409 |
| API-034 | DELETE | /api/v1/apns/:id | — | — | JWT(tenant_admin) | 204, 422 |
| API-081 | POST | /api/v1/ip-pools | `{apn_id,name,cidr_v4?,cidr_v6?,alert_threshold_warning,reclaim_grace_period_days}` | `{id,name,total_addresses,...}` | JWT(tenant_admin+) | 201, 400 |
| API-085 | POST | /api/v1/ip-pools/:id/addresses/reserve | `{sim_id,address_v4?}` | `{id,address_v4,sim_id,allocation_type:"static"}` | JWT(sim_manager+) | 201, 409, 422 |

## Dependencies
- Blocked by: STORY-009 (operators must exist)
- Blocks: STORY-011 (SIM creation needs APN + IP pool)

## Test Scenarios
- [ ] Create APN with valid operator → 201
- [ ] Delete APN with active SIMs → 422 APN_HAS_ACTIVE_SIMS
- [ ] Delete APN with no SIMs → 204, state = ARCHIVED
- [ ] Create IP pool from 10.0.0.0/24 → 254 usable addresses generated
- [ ] Allocate IP → pool used_addresses incremented
- [ ] Allocate from exhausted pool → 422 POOL_EXHAUSTED
- [ ] Reserve static IP for SIM → IP locked, not returned to pool

## Effort Estimate
- Size: L
- Complexity: High
