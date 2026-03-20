# Deliverable: STORY-003 — Authentication: JWT + Refresh Token + 2FA

**Date:** 2026-03-20
**Status:** Complete

## Summary

Implemented complete JWT authentication with refresh tokens, account lockout, TOTP-based 2FA, and gateway auth middleware.

## New Files

| File | Purpose |
|------|---------|
| `internal/auth/jwt.go` | JWT generation (HS256) and validation, Claims struct (UserID, TenantID, Role, Partial) |
| `internal/auth/totp.go` | TOTP secret generation and code validation (pquerna/otp) |
| `internal/auth/auth.go` | Auth service: Login, Refresh, Logout, Setup2FA, Verify2FA with lockout logic |
| `internal/auth/auth_test.go` | Service-level unit tests (login flows, lockout, 2FA, refresh, logout) |
| `internal/auth/jwt_test.go` | JWT generation/validation unit tests |
| `internal/store/user.go` | UserStore (GetByEmail, GetByID, login tracking, TOTP) + SessionStore (CRUD, revoke) |
| `internal/api/auth/handler.go` | HTTP handlers for 5 auth endpoints with httpOnly cookie management |
| `internal/gateway/auth_middleware.go` | JWT extraction middleware (JWTAuth + JWTAuthAllowPartial) |

## Modified Files

| File | Change |
|------|--------|
| `internal/apierr/apierr.go` | Added 6 auth error codes (INVALID_CREDENTIALS, ACCOUNT_LOCKED, etc.) |
| `internal/gateway/router.go` | Registered auth routes (public, partial-2FA, authenticated groups) |
| `cmd/argus/main.go` | Wired UserStore, SessionStore, auth.Service, AuthHandler |
| `internal/config/config.go` | Added JWTIssuer, LoginMaxAttempts, LoginLockoutDuration |
| `go.mod` / `go.sum` | Added golang-jwt/jwt/v5, pquerna/otp, google/uuid, golang.org/x/crypto |

## Endpoints Implemented

| Ref | Method | Path | Auth |
|-----|--------|------|------|
| API-001 | POST | /api/v1/auth/login | None |
| API-002 | POST | /api/v1/auth/refresh | Cookie |
| API-003 | POST | /api/v1/auth/logout | JWT |
| API-004 | POST | /api/v1/auth/2fa/setup | JWT |
| API-005 | POST | /api/v1/auth/2fa/verify | Partial JWT |

## Architecture References Fulfilled

- SVC-01: Gateway auth middleware
- SVC-03: Auth handlers
- API-001 to API-005
- TBL-02 (users), TBL-03 (user_sessions)

## Test Results

- 18 unit tests, all passing
- Full suite (12 packages) passing, no regressions
- Gate: PASS (1 critical security fix applied — partial JWT protection)

## Gate Fixes Applied

1. **CRITICAL**: JWTAuth middleware now rejects partial (2FA-pending) tokens
2. **MINOR**: Removed unnecessary Content-Type header on 204 logout response
