# Deliverable: STORY-005 — Tenant Management & User CRUD

**Date:** 2026-03-20
**Status:** Complete

## Summary

Implemented full CRUD for tenants (super_admin only) and users (tenant_admin within own tenant), with resource limit enforcement, cursor-based pagination, state transitions, and audit logging.

## New Files

| File | Purpose |
|------|---------|
| `internal/store/tenant.go` | TenantStore: Create, GetByID, List (cursor pagination), Update, GetStats, CountUsersByTenant |
| `internal/store/tenant_test.go` | Tenant store unit tests |
| `internal/api/tenant/handler.go` | Tenant handler: List, Create, Get, Update, Stats (API-010 to API-014) |
| `internal/api/tenant/handler_test.go` | Tenant handler tests |
| `internal/api/user/handler.go` | User handler: List, Create, Update (API-006 to API-008) |
| `internal/api/user/handler_test.go` | User handler tests |

## Modified Files

| File | Change |
|------|--------|
| `internal/store/user.go` | Added CreateUser, ListByTenant, UpdateUser, CountByTenant |
| `internal/apierr/apierr.go` | Added CodeResourceLimitExceeded, CodeTenantSuspended, RoleLevel(), HasRole() |
| `internal/gateway/rbac.go` | Delegated RoleLevel/HasRole to apierr (import cycle fix) |
| `internal/gateway/router.go` | Added tenant + user route groups with auth middleware |
| `cmd/argus/main.go` | Wired TenantStore, TenantHandler, UserHandler |

## Endpoints Implemented

| Ref | Method | Path | Auth |
|-----|--------|------|------|
| API-010 | GET | /api/v1/tenants | super_admin |
| API-011 | POST | /api/v1/tenants | super_admin |
| API-012 | GET | /api/v1/tenants/:id | super_admin or own tenant |
| API-013 | PATCH | /api/v1/tenants/:id | super_admin or tenant_admin |
| API-014 | GET | /api/v1/tenants/:id/stats | tenant_admin+ |
| API-006 | GET | /api/v1/users | tenant_admin+ |
| API-007 | POST | /api/v1/users | tenant_admin+ |
| API-008 | PATCH | /api/v1/users/:id | tenant_admin+ or self |

## Test Results

- 21 unit tests, all passing
- Full suite passing, no regressions
- Gate: PASS (0 fixes needed)
