# Error Code Catalog — Argus

> All API errors use the standard error envelope. Every error has a unique code, fixed HTTP status, and deterministic structure.
> Error codes are string constants defined in `internal/apierr/apierr.go`.
>
> **Exception:** 5G SBA endpoints (`internal/aaa/sba/`, port :8443) use `application/problem+json` error format per 3GPP TS 29.500 instead of the Argus standard envelope. This is correct for NF-to-NF communication. See [PROTOCOLS.md](PROTOCOLS.md) for SBA error details.

## Standard Error Envelope

```json
{
  "status": "error",
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable description of what went wrong",
    "details": []
  }
}
```

- `code`: Machine-readable constant (UPPER_SNAKE_CASE). Use this for programmatic error handling.
- `message`: Human-readable string. MAY vary between occurrences. Do NOT parse this.
- `details`: Optional array of structured detail objects. Present for validation errors and some domain errors.

---

## Auth Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `INVALID_CREDENTIALS` | 401 | Login failed: wrong email/password or invalid API key | `{"status":"error","error":{"code":"INVALID_CREDENTIALS","message":"Invalid email or password"}}` |
| `ACCOUNT_LOCKED` | 403 | Account locked after too many failed login attempts (5 consecutive failures, 15min lockout) | `{"status":"error","error":{"code":"ACCOUNT_LOCKED","message":"Account locked due to too many failed attempts. Try again in 12 minutes."}}` |
| `ACCOUNT_DISABLED` | 403 | Account explicitly disabled by tenant admin | `{"status":"error","error":{"code":"ACCOUNT_DISABLED","message":"Your account has been disabled. Contact your administrator."}}` |
| `INVALID_2FA_CODE` | 401 | TOTP code is incorrect or expired | `{"status":"error","error":{"code":"INVALID_2FA_CODE","message":"Invalid or expired 2FA code"}}` |
| `TOKEN_EXPIRED` | 401 | JWT access token has expired; client should refresh | `{"status":"error","error":{"code":"TOKEN_EXPIRED","message":"Access token has expired. Use refresh token to obtain a new one."}}` |
| `INVALID_REFRESH_TOKEN` | 401 | Refresh token is invalid, expired, revoked, or already used | `{"status":"error","error":{"code":"INVALID_REFRESH_TOKEN","message":"Refresh token is invalid or has been revoked"}}` |

### Auth Error Details

For `ACCOUNT_LOCKED`, the `details` array includes retry information:
```json
{
  "status": "error",
  "error": {
    "code": "ACCOUNT_LOCKED",
    "message": "Account locked due to too many failed attempts. Try again in 12 minutes.",
    "details": [
      { "retry_after_seconds": 720, "failed_attempts": 5 }
    ]
  }
}
```

---

## Authorization Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `FORBIDDEN` | 403 | Generic authorization failure; user cannot perform this action | `{"status":"error","error":{"code":"FORBIDDEN","message":"You do not have permission to perform this action"}}` |
| `INSUFFICIENT_ROLE` | 403 | User's role is below the minimum required for this endpoint | `{"status":"error","error":{"code":"INSUFFICIENT_ROLE","message":"This action requires tenant_admin role or higher","details":[{"required_role":"tenant_admin","current_role":"sim_manager"}]}}` |
| `SCOPE_DENIED` | 403 | API key does not have the required scope for this endpoint | `{"status":"error","error":{"code":"SCOPE_DENIED","message":"API key does not have the required scope","details":[{"required_scope":"sims:write","available_scopes":["sims:read","analytics:read"]}]}}` |

---

## Validation Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `VALIDATION_ERROR` | 422 | Request body or query parameters failed validation. `details` array lists all field errors. | See below |
| `INVALID_FORMAT` | 400 | Request body is malformed (not valid JSON, wrong content-type, etc.) | `{"status":"error","error":{"code":"INVALID_FORMAT","message":"Request body is not valid JSON"}}` |

### Validation Error Details

The `details` array contains one entry per invalid field. ALL validation errors are returned at once (not one-at-a-time):

```json
{
  "status": "error",
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": [
      { "field": "iccid", "message": "ICCID is required", "code": "required" },
      { "field": "iccid", "message": "ICCID must be 19-22 digits", "code": "format" },
      { "field": "imsi", "message": "IMSI must be 15 digits", "code": "format" },
      { "field": "operator_id", "message": "Operator not found", "code": "invalid_reference" },
      { "field": "metadata.custom_field", "message": "Value exceeds maximum length of 255", "code": "max_length" }
    ]
  }
}
```

Detail `code` values: `required`, `format`, `min_length`, `max_length`, `min_value`, `max_value`, `invalid_reference`, `invalid_enum`, `unique_violation`.

---

## Resource Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `NOT_FOUND` | 404 | Requested resource does not exist (or is not visible to the current tenant) | `{"status":"error","error":{"code":"NOT_FOUND","message":"Resource not found"}}` |
| `ALREADY_EXISTS` | 409 | Attempting to create a resource that already exists (unique constraint) | `{"status":"error","error":{"code":"ALREADY_EXISTS","message":"A resource with this identifier already exists","details":[{"field":"name","value":"iot.fleet"}]}}` |
| `CONFLICT` | 409 | Optimistic concurrency conflict or state conflict preventing operation | `{"status":"error","error":{"code":"CONFLICT","message":"Resource was modified by another request. Retry with latest version."}}` |

---

## SIM Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `SIM_NOT_FOUND` | 404 | SIM with given ID/ICCID/IMSI does not exist in this tenant | `{"status":"error","error":{"code":"SIM_NOT_FOUND","message":"SIM not found","details":[{"field":"iccid","value":"89901112345678901234"}]}}` |
| `INVALID_STATE_TRANSITION` | 422 | Requested SIM state change is not allowed per the state machine | `{"status":"error","error":{"code":"INVALID_STATE_TRANSITION","message":"Cannot transition SIM from 'terminated' to 'active'","details":[{"from_state":"terminated","to_state":"active","allowed_transitions":["purged"]}]}}` |
| `ICCID_EXISTS` | 409 | ICCID already registered in the system | `{"status":"error","error":{"code":"ICCID_EXISTS","message":"A SIM with ICCID 89901112345678901234 already exists"}}` |
| `IMSI_EXISTS` | 409 | IMSI already registered in the system | `{"status":"error","error":{"code":"IMSI_EXISTS","message":"A SIM with IMSI 286010123456789 already exists"}}` |
| `SIM_HAS_ACTIVE_SESSION` | 422 | Operation cannot proceed because SIM has an active AAA session (e.g., certain state changes require session disconnect first) | `{"status":"error","error":{"code":"SIM_HAS_ACTIVE_SESSION","message":"SIM has an active session. Disconnect the session first or use force option.","details":[{"session_id":"abc-123","started_at":"2026-03-18T10:00:00Z"}]}}` |

---

## APN Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `APN_NOT_FOUND` | 404 | APN with given ID does not exist in this tenant | `{"status":"error","error":{"code":"APN_NOT_FOUND","message":"APN not found"}}` |
| `APN_HAS_ACTIVE_SIMS` | 422 | Cannot archive/delete APN because it has SIMs in non-terminated state | `{"status":"error","error":{"code":"APN_HAS_ACTIVE_SIMS","message":"Cannot archive APN with active SIMs. Move or terminate all SIMs first.","details":[{"active_sim_count":12345}]}}` |
| `APN_ARCHIVED` | 422 | Cannot assign SIMs to an archived APN | `{"status":"error","error":{"code":"APN_ARCHIVED","message":"APN is archived and cannot accept new SIM assignments"}}` |

---

## Operator Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `OPERATOR_NOT_FOUND` | 404 | Operator with given ID does not exist or tenant has no grant | `{"status":"error","error":{"code":"OPERATOR_NOT_FOUND","message":"Operator not found or access not granted"}}` |
| `OPERATOR_DOWN` | 503 | Operator adapter is currently unreachable; health check failing | `{"status":"error","error":{"code":"OPERATOR_DOWN","message":"Operator 'Turkcell' is currently unavailable","details":[{"operator_id":"uuid","health_status":"down","last_check_at":"2026-03-18T14:00:00Z"}]}}` |
| `CIRCUIT_OPEN` | 503 | Circuit breaker for this operator is open; requests are being rejected | `{"status":"error","error":{"code":"CIRCUIT_OPEN","message":"Circuit breaker is open for operator 'Turkcell'. Recovery in progress.","details":[{"operator_id":"uuid","open_since":"2026-03-18T13:55:00Z","estimated_recovery":"2026-03-18T14:05:00Z"}]}}` |
| `FAILOVER_EXHAUSTED` | 503 | All failover operators have been tried and none are available | `{"status":"error","error":{"code":"FAILOVER_EXHAUSTED","message":"All failover operators exhausted. No available operator to handle the request."}}` |

---

## MSISDN Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `MSISDN_NOT_FOUND` | 404 | MSISDN pool entry with given ID does not exist in this tenant | `{"status":"error","error":{"code":"MSISDN_NOT_FOUND","message":"MSISDN not found"}}` |
| `MSISDN_NOT_AVAILABLE` | 409 | MSISDN is not in 'available' state and cannot be assigned to a SIM | `{"status":"error","error":{"code":"MSISDN_NOT_AVAILABLE","message":"MSISDN is not available for assignment"}}` |

---

## IP Pool Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `POOL_EXHAUSTED` | 422 | IP pool has no available addresses (100% utilization) | `{"status":"error","error":{"code":"POOL_EXHAUSTED","message":"IP pool 'fleet-pool-v4' is exhausted. No available addresses.","details":[{"pool_id":"uuid","total_addresses":65534,"used_addresses":65534,"utilization_pct":100}]}}` |
| `IP_CONFLICT` | 409 | IP address is already allocated to another SIM | `{"status":"error","error":{"code":"IP_CONFLICT","message":"IP address 10.0.1.42 is already allocated","details":[{"address":"10.0.1.42","allocated_to_sim_id":"uuid"}]}}` |
| `IP_ALREADY_ALLOCATED` | 409 | Attempting to reserve an IP that is already in reserved/allocated state | `{"status":"error","error":{"code":"IP_ALREADY_ALLOCATED","message":"IP address 10.0.1.42 is already allocated"}}` |

---

## Policy Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `POLICY_NOT_FOUND` | 404 | Policy or policy version not found | `{"status":"error","error":{"code":"POLICY_NOT_FOUND","message":"Policy not found"}}` |
| `DSL_SYNTAX_ERROR` | 422 | Policy DSL has syntax errors; cannot be parsed | `{"status":"error","error":{"code":"DSL_SYNTAX_ERROR","message":"Policy DSL syntax error","details":[{"line":7,"column":12,"message":"Expected '{' after MATCH keyword","snippet":"  MATCH\n       ^"}]}}` |
| `DSL_COMPILE_ERROR` | 422 | Policy DSL is syntactically valid but semantically invalid (unknown identifiers, type mismatches, etc.) | `{"status":"error","error":{"code":"DSL_COMPILE_ERROR","message":"Policy DSL compilation error","details":[{"line":8,"message":"Unknown condition 'location'. Supported: usage, time_of_day, rat_type, apn, operator, roaming, session_count, bandwidth_used"}]}}` |
| `ROLLOUT_IN_PROGRESS` | 422 | Cannot start a new rollout while one is already in progress for this policy | `{"status":"error","error":{"code":"ROLLOUT_IN_PROGRESS","message":"A rollout is already in progress for this policy","details":[{"rollout_id":"uuid","current_stage":2,"state":"in_progress"}]}}` |
| `VERSION_NOT_DRAFT` | 422 | Attempted to modify or activate a policy version that is not in draft state | `{"status":"error","error":{"code":"VERSION_NOT_DRAFT","message":"Policy version is not in draft state and cannot be modified","details":[{"version_id":"uuid","current_state":"active"}]}}` |

---

## eSIM Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `PROFILE_NOT_FOUND` | 404 | eSIM profile not found | `{"status":"error","error":{"code":"PROFILE_NOT_FOUND","message":"eSIM profile not found"}}` |
| `PROFILE_ALREADY_ENABLED` | 422 | Attempting to enable a profile that is already enabled | `{"status":"error","error":{"code":"PROFILE_ALREADY_ENABLED","message":"eSIM profile is already in enabled state"}}` |
| `SM_DP_PLUS_ERROR` | 502 | Error communicating with SM-DP+ server | `{"status":"error","error":{"code":"SM_DP_PLUS_ERROR","message":"SM-DP+ server returned an error","details":[{"sm_dp_error_code":"8.1","sm_dp_message":"Profile not available"}]}}` |
| `SWITCH_FAILED` | 502 | eSIM operator switch failed during the multi-step process | `{"status":"error","error":{"code":"SWITCH_FAILED","message":"eSIM operator switch failed","details":[{"step":"disable_current","error":"Timeout communicating with Turkcell SM-DP+"}]}}` |

---

## Job Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `JOB_NOT_FOUND` | 404 | Job with given ID not found | `{"status":"error","error":{"code":"JOB_NOT_FOUND","message":"Job not found"}}` |
| `JOB_ALREADY_RUNNING` | 409 | Cannot start/retry a job that is already in running state | `{"status":"error","error":{"code":"JOB_ALREADY_RUNNING","message":"Job is already running","details":[{"job_id":"uuid","state":"running","progress_pct":45.5}]}}` |
| `JOB_CANCELLED` | 422 | Cannot operate on a cancelled job | `{"status":"error","error":{"code":"JOB_CANCELLED","message":"Job has been cancelled and cannot be retried"}}` |

---

## Tenant Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `RESOURCE_LIMIT_EXCEEDED` | 422 | Tenant has reached a resource limit (max SIMs, APNs, users, etc.) | `{"status":"error","error":{"code":"RESOURCE_LIMIT_EXCEEDED","message":"Tenant resource limit exceeded","details":[{"resource":"sims","current":1000000,"limit":1000000}]}}` |
| `TENANT_SUSPENDED` | 403 | Tenant account is suspended; all API operations blocked except read-only | `{"status":"error","error":{"code":"TENANT_SUSPENDED","message":"Tenant account is suspended. Contact system administrator."}}` |

---

## Rate Limit Error

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `RATE_LIMITED` | 429 | Request rate limit exceeded | See below |

Rate limit response includes `Retry-After` HTTP header and limit details:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 32
X-RateLimit-Limit: 1000
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1710770520
Content-Type: application/json

{
  "status": "error",
  "error": {
    "code": "RATE_LIMITED",
    "message": "Rate limit exceeded. Retry after 32 seconds.",
    "details": [
      {
        "limit": 1000,
        "window": "per_minute",
        "retry_after_seconds": 32
      }
    ]
  }
}
```

---

## System Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `INTERNAL_ERROR` | 500 | Unexpected server error. Details are logged server-side with correlation_id but NOT exposed to client. | `{"status":"error","error":{"code":"INTERNAL_ERROR","message":"An unexpected error occurred. Reference: f47ac10b-58cc-4372-a567-0e02b2c3d479"}}` |
| `SERVICE_UNAVAILABLE` | 503 | Server is temporarily unavailable (overloaded, shutting down, dependency down) | `{"status":"error","error":{"code":"SERVICE_UNAVAILABLE","message":"Service temporarily unavailable. Please retry."}}` |
| `DATABASE_ERROR` | 503 | Database connection failed or query timed out. Logged server-side with details. | `{"status":"error","error":{"code":"DATABASE_ERROR","message":"Database operation failed. Reference: f47ac10b-58cc-4372-a567-0e02b2c3d479"}}` |

---

## Error Code Constants (Go)

All error codes are defined as constants in a single file for easy reference:

```go
package errors

const (
    // Auth
    CodeInvalidCredentials  = "INVALID_CREDENTIALS"
    CodeAccountLocked       = "ACCOUNT_LOCKED"
    CodeAccountDisabled     = "ACCOUNT_DISABLED"
    CodeInvalid2FACode      = "INVALID_2FA_CODE"
    CodeTokenExpired        = "TOKEN_EXPIRED"
    CodeInvalidRefreshToken = "INVALID_REFRESH_TOKEN"

    // Authorization
    CodeForbidden        = "FORBIDDEN"
    CodeInsufficientRole = "INSUFFICIENT_ROLE"
    CodeScopeDenied      = "SCOPE_DENIED"

    // Validation
    CodeValidationError = "VALIDATION_ERROR"
    CodeInvalidFormat   = "INVALID_FORMAT"

    // Resource
    CodeNotFound      = "NOT_FOUND"
    CodeAlreadyExists = "ALREADY_EXISTS"
    CodeConflict      = "CONFLICT"

    // SIM
    CodeSIMNotFound             = "SIM_NOT_FOUND"
    CodeInvalidStateTransition  = "INVALID_STATE_TRANSITION"
    CodeICCIDExists             = "ICCID_EXISTS"
    CodeIMSIExists              = "IMSI_EXISTS"
    CodeSIMHasActiveSession     = "SIM_HAS_ACTIVE_SESSION"

    // APN
    CodeAPNNotFound      = "APN_NOT_FOUND"
    CodeAPNHasActiveSIMs = "APN_HAS_ACTIVE_SIMS"
    CodeAPNArchived      = "APN_ARCHIVED"

    // Operator
    CodeOperatorNotFound  = "OPERATOR_NOT_FOUND"
    CodeOperatorDown      = "OPERATOR_DOWN"
    CodeCircuitOpen       = "CIRCUIT_OPEN"
    CodeFailoverExhausted = "FAILOVER_EXHAUSTED"

    // MSISDN
    CodeMSISDNNotFound     = "MSISDN_NOT_FOUND"
    CodeMSISDNNotAvailable = "MSISDN_NOT_AVAILABLE"

    // IP Pool
    CodePoolExhausted    = "POOL_EXHAUSTED"
    CodeIPConflict       = "IP_CONFLICT"
    CodeIPAlreadyAllocated = "IP_ALREADY_ALLOCATED"

    // Policy
    CodePolicyNotFound     = "POLICY_NOT_FOUND"
    CodeDSLSyntaxError     = "DSL_SYNTAX_ERROR"
    CodeDSLCompileError    = "DSL_COMPILE_ERROR"
    CodeRolloutInProgress  = "ROLLOUT_IN_PROGRESS"
    CodeVersionNotDraft    = "VERSION_NOT_DRAFT"

    // eSIM
    CodeProfileNotFound      = "PROFILE_NOT_FOUND"
    CodeProfileAlreadyEnabled = "PROFILE_ALREADY_ENABLED"
    CodeSMDPPlusError        = "SM_DP_PLUS_ERROR"
    CodeSwitchFailed         = "SWITCH_FAILED"

    // Job
    CodeJobNotFound       = "JOB_NOT_FOUND"
    CodeJobAlreadyRunning = "JOB_ALREADY_RUNNING"
    CodeJobCancelled      = "JOB_CANCELLED"

    // Tenant
    CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"
    CodeTenantSuspended       = "TENANT_SUSPENDED"

    // Rate Limit
    CodeRateLimited = "RATE_LIMITED"

    // System
    CodeInternalError       = "INTERNAL_ERROR"
    CodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
    CodeDatabaseError       = "DATABASE_ERROR"
)
```

## Client Error Handling Guidelines

1. **Always check `error.code`** for programmatic handling, never parse `error.message`.
2. **Display `error.message`** to users as-is; it is safe and localization-ready.
3. **On 401 TOKEN_EXPIRED**: silently attempt token refresh via `POST /api/v1/auth/refresh`. On refresh failure, redirect to login.
4. **On 401 INVALID_REFRESH_TOKEN**: clear all tokens, redirect to login.
5. **On 429 RATE_LIMITED**: read `Retry-After` header, implement exponential backoff.
6. **On 422 VALIDATION_ERROR**: map `details[].field` to form fields for inline error display.
7. **On 500/503**: show generic error with the correlation_id from the message for support reference.
8. **On 409 CONFLICT**: refetch the resource and retry or prompt the user.
