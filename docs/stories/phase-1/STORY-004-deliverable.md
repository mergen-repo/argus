# Deliverable: STORY-004 — RBAC Middleware & Permission Enforcement

**Date:** 2026-03-20
**Status:** Complete

## Summary

Implemented role-based access control middleware with 7-level role hierarchy, RequireRole and RequireScope middleware functions, and applied to existing routes.

## New Files

| File | Purpose |
|------|---------|
| `internal/gateway/rbac.go` | Role hierarchy map, RoleLevel(), HasRole(), RequireRole(), RequireScope() middleware |
| `internal/gateway/rbac_test.go` | 12 test functions covering role levels, hierarchy, RequireRole, RequireScope, full matrix |

## Modified Files

| File | Change |
|------|--------|
| `internal/apierr/apierr.go` | Added CodeForbidden, CodeInsufficientRole, CodeScopeDenied, AuthTypeKey, ScopesKey |
| `internal/gateway/router.go` | Applied RequireRole("api_user") to authenticated route group |

## Architecture References Fulfilled

- SVC-01: Gateway RBAC middleware
- TBL-02: users.role field used via JWT claims

## Role Hierarchy

| Level | Role |
|-------|------|
| 7 | super_admin |
| 6 | tenant_admin |
| 5 | operator_manager |
| 4 | sim_manager |
| 3 | policy_editor |
| 2 | analyst |
| 1 | api_user |

## Test Results

- 12 test functions, all passing (80+ sub-cases including 7x7 matrix)
- Full suite passing, no regressions
- Gate: PASS (1 fix applied — error message clarity)

## Known Limitation

Linear role hierarchy cannot fully represent the non-linear ARCHITECTURE.md RBAC matrix (e.g., operator_manager cannot manage SIMs but sim_manager can, despite lower level). Will need permission-based refinement when SIM/policy stories are implemented.
