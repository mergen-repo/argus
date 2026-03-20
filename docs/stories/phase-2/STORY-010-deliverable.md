# Deliverable: STORY-010 — APN CRUD & IP Pool Management

## Summary
Implemented APN management (CRUD, archive with active SIM check) and IP Pool management (CRUD, CIDR-based bulk IP generation, transactional allocation/reservation/release with reclaim grace period).

## Files Changed

### New Files
- `internal/store/apn.go` — APNStore: Create, GetByID, GetByName, List, Update, Archive, CountByTenant
- `internal/store/ippool.go` — IPPoolStore: Create (bulk IP gen from CIDR), GetByID, List, Update, ListAddresses, ReserveStaticIP, AllocateIP, ReleaseIP
- `internal/api/apn/handler.go` — APN handlers: List (API-030), Create (API-031), Get (API-032), Update (API-033), Archive (API-034)
- `internal/api/ippool/handler.go` — IPPool handlers: List (API-040), Create (API-041), Get (API-042), Update (API-043), ListAddresses (API-044), ReserveIP (API-045), AllocateIP, ReleaseIP
- `internal/store/apn_test.go` — APN store tests
- `internal/api/apn/handler_test.go` — APN handler validation tests
- `internal/api/ippool/handler_test.go` — IPPool handler validation tests

### Modified Files
- `internal/store/stubs.go` — Removed APN and IPPool stubs
- `internal/gateway/router.go` — APN and IPPool route groups with RBAC
- `cmd/argus/main.go` — Wired APNStore, IPPoolStore, handlers
- `internal/job/import.go` — Updated to use new store interfaces
- `internal/apierr/apierr.go` — Added CodeAPNHasActiveSIMs, CodePoolExhausted, CodeIPAlreadyAllocated

## Architecture References Fulfilled
- API-030 to API-034: APN endpoints
- API-040 to API-045: IP Pool endpoints
- TBL-03 (apns), TBL-09 (ip_pools), TBL-10 (ip_addresses) fully utilized
- ALGORITHMS.md Section 1: IP allocation algorithm with FOR UPDATE SKIP LOCKED
- G-016: APN deletion rules (hard block if active SIMs)
- G-024: IPAM pool utilization alerts, conflict detection, static IP reservation

## Test Coverage
- 35 unit tests across 4 packages (29 full suite packages pass)
- APN: handler validation, name too long, duplicate name, archive checks
- IPPool: handler validation, CIDR format, threshold ranges, name too long
- IP generation: IPv4 and IPv6 CIDR parsing
