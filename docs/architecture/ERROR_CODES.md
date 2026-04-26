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
| `PASSWORD_CHANGE_REQUIRED` | 403 | Login succeeded but password change is mandatory; partial JWT issued; only change-password endpoint accessible | `{"status":"ok","data":{"partial":true,"reason":"password_change_required"},"meta":{"code":"PASSWORD_CHANGE_REQUIRED"}}` |
| `PASSWORD_TOO_SHORT` | 422 | Password does not meet minimum length requirement (`PASSWORD_MIN_LENGTH`) | `{"status":"error","error":{"code":"PASSWORD_TOO_SHORT","message":"Password must be at least 12 characters"}}` |
| `PASSWORD_MISSING_CLASS` | 422 | Password missing required character class (upper, lower, digit, or symbol) | `{"status":"error","error":{"code":"PASSWORD_MISSING_CLASS","message":"Password must contain uppercase, lowercase, digit, and symbol"}}` |
| `PASSWORD_REPEATING_CHARS` | 422 | Password has too many consecutive identical characters (`PASSWORD_MAX_REPEATING`) | `{"status":"error","error":{"code":"PASSWORD_REPEATING_CHARS","message":"Password must not have more than 3 consecutive identical characters"}}` |
| `PASSWORD_REUSED` | 422 | New password matches one of the last N password hashes (`PASSWORD_HISTORY_COUNT`) | `{"status":"error","error":{"code":"PASSWORD_REUSED","message":"Password was used recently. Choose a different password."}}` |
| `API_KEY_IP_NOT_ALLOWED` | 403 | Request IP is not in the API key's allowed_ips CIDR whitelist | `{"status":"error","error":{"code":"API_KEY_IP_NOT_ALLOWED","message":"Request IP not in API key whitelist"}}` |
| `PASSWORD_RESET_INVALID_TOKEN` | 400 | Reset link is invalid, has been used, or has expired. Used by FIX-228 password reset confirm endpoint (`POST /api/v1/auth/password-reset/confirm`). | `{"status":"error","error":{"code":"PASSWORD_RESET_INVALID_TOKEN","message":"Reset link is invalid, has been used, or has expired."}}` |

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
| `FORBIDDEN_CROSS_TENANT` | 403 | Cross-tenant resource access denied (reserved for explicit cross-tenant flows; default behavior is 404 SIM_NOT_FOUND to prevent ID enumeration) | `{"status":"error","error":{"code":"FORBIDDEN_CROSS_TENANT","message":"Cross-tenant resource access denied"}}` |

---

## Validation Errors

| Code | HTTP Status | Description | Example Response |
|------|-------------|-------------|------------------|
| `VALIDATION_ERROR` | 422 | Request body or query parameters failed validation. `details` array lists all field errors. | See below |
| `INVALID_FORMAT` | 400 | Request body is malformed (not valid JSON, wrong content-type, etc.) | `{"status":"error","error":{"code":"INVALID_FORMAT","message":"Request body is not valid JSON"}}` |
| `INVALID_REFERENCE` | 400 | A foreign-key reference in the request body points at a non-existent record. Defensive duplicate of handler-layer NOT_FOUND checks — emitted only when the FK would otherwise violate at DB (SQLSTATE 23503). `details[0].field` names the offending column (`operator_id`, `apn_id`, `ip_address_id`); `details[0].constraint` names the DB constraint. Added FIX-206. | `{"status":"error","error":{"code":"INVALID_REFERENCE","message":"operator_id does not reference an existing operator","details":[{"field":"operator_id","constraint":"fk_sims_operator"}]}}` |
| `INVALID_IMSI_FORMAT` | 400 | IMSI does not conform to PLMN format (`^\d{14,15}$`). Emitted when `IMSI_STRICT_VALIDATION=true` (default) and the IMSI fails the regex at API or AAA ingestion boundary. Added FIX-207. | See below |
| `INVALID_SEVERITY` | 400 | Severity value is not in the canonical taxonomy (`critical`, `high`, `medium`, `low`, `info`). Sent by every endpoint that accepts a severity filter or severity payload field. Added FIX-211. | `{"status":"error","error":{"code":"INVALID_SEVERITY","message":"severity must be one of: critical, high, medium, low, info; got 'warning'"}}` |
| `INVALID_PARAM` | 400 | A query parameter value is syntactically valid but out of accepted range or semantics (e.g. `rollout_stage_pct` out of 1–100 range, `limit` > max). Distinct from `INVALID_FORMAT` (malformed body) and `VALIDATION_ERROR` (422 body field errors). Added FIX-233. | `{"status":"error","error":{"code":"INVALID_PARAM","message":"rollout_stage_pct must be between 1 and 100"}}` |

### INVALID_IMSI_FORMAT Details

```json
{
  "status": "error",
  "error": {
    "code": "INVALID_IMSI_FORMAT",
    "message": "IMSI format is invalid",
    "details": [{"field": "imsi", "value": "abc123", "expected": "^\\d{14,15}$"}]
  }
}
```

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
| `ALERT_NOT_FOUND` | 404 | Alert does not exist in the tenant's scope (or never existed). Same shape for cross-tenant lookups — never reveals existence. | `{"status":"error","error":{"code":"ALERT_NOT_FOUND","message":"alert not found"}}` |
| `ALERT_NO_DATA` | 404 | Export request matched zero alerts (no rows to export). Returned by CSV/JSON/PDF export endpoints when filters produce an empty result set. FIX-229. | `{"status":"error","error":{"code":"ALERT_NO_DATA","message":"no alerts match the requested filters"}}` |
| `SUPPRESSION_NOT_FOUND` | 404 | Suppression with given ID does not exist in this tenant (or never existed). Returned by `DELETE /alerts/suppressions/{id}`. FIX-229. | `{"status":"error","error":{"code":"SUPPRESSION_NOT_FOUND","message":"alert suppression not found"}}` |
| `DUPLICATE` | 409 | Tenant-scoped uniqueness conflict for a named entity. Currently used for duplicate suppression `rule_name` on `POST /alerts/suppressions`. Distinct from the generic `ALREADY_EXISTS` to match the plan spec. FIX-229. | `{"status":"error","error":{"code":"DUPLICATE","message":"a suppression rule with this name already exists"}}` |

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
| `PROTOCOL_NOT_CONFIGURED` | 422 | Operator has no enabled protocol adapters (zero `adapter_config` sub-keys with `enabled: true`), or the requested protocol is not configured on this operator — returned by `POST /api/v1/operators/:id/test` (legacy) and `POST /api/v1/operators/:id/test/:protocol` (per-protocol). Added STORY-090 Gate F-A3 (VAL-030). | `{"status":"error","error":{"code":"PROTOCOL_NOT_CONFIGURED","message":"Protocol radius is not configured for this operator"}}` |

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
| `NOT_ESIM` | 422 | Operation requires an eSIM-capable SIM but the target SIM is not eSIM type | `{"status":"error","error":{"code":"NOT_ESIM","message":"SIM is not an eSIM type"}}` |
| `INVALID_PROFILE_STATE` | 422 | Profile is in a state that does not allow the requested operation | `{"status":"error","error":{"code":"INVALID_PROFILE_STATE","message":"Profile state does not allow this operation","details":[{"profile_id":"uuid","current_state":"deleted"}]}}` |
| `SAME_PROFILE` | 422 | Switch target profile is the same as the currently enabled profile | `{"status":"error","error":{"code":"SAME_PROFILE","message":"Target profile is already the active profile"}}` |
| `DIFFERENT_SIM` | 422 | Profile does not belong to the target SIM | `{"status":"error","error":{"code":"DIFFERENT_SIM","message":"Profile does not belong to the specified SIM"}}` |
| `SM_DP_PLUS_ERROR` | 502 | Error communicating with SM-DP+ server | `{"status":"error","error":{"code":"SM_DP_PLUS_ERROR","message":"SM-DP+ server returned an error","details":[{"sm_dp_error_code":"8.1","sm_dp_message":"Profile not available"}]}}` |
| `SWITCH_FAILED` | 502 | eSIM operator switch failed during the multi-step process | `{"status":"error","error":{"code":"SWITCH_FAILED","message":"eSIM operator switch failed","details":[{"step":"disable_current","error":"Timeout communicating with Turkcell SM-DP+"}]}}` |
| `PROFILE_LIMIT_EXCEEDED` | 422 | Max profiles per SIM reached (GSMA SGP.22 limit: 8 profiles per SIM) | `{"status":"error","error":{"code":"PROFILE_LIMIT_EXCEEDED","message":"Maximum number of profiles per SIM reached","details":[{"sim_id":"uuid","current_count":8,"limit":8}]}}` |
| `CANNOT_DELETE_ENABLED_PROFILE` | 409 | Cannot delete a profile that is currently in enabled state | `{"status":"error","error":{"code":"CANNOT_DELETE_ENABLED_PROFILE","message":"Cannot delete a profile in enabled state. Disable it first."}}` |
| `DUPLICATE_PROFILE` | 409 | Profile with same sim_id and profile_id combination already exists | `{"status":"error","error":{"code":"DUPLICATE_PROFILE","message":"A profile with this profile_id already exists for this SIM","details":[{"sim_id":"uuid","profile_id":"abc123"}]}}` |
| `PROFILE_NOT_AVAILABLE` | 422 | Profile is not in available or disabled state and cannot be enabled | `{"status":"error","error":{"code":"PROFILE_NOT_AVAILABLE","message":"Profile must be in available or disabled state to enable","details":[{"profile_id":"uuid","current_state":"deleted"}]}}` |
| `IP_RELEASE_FAILED` | — (warning, non-blocking) | IP release failed during profile switch; operation continues | `{"status":"error","error":{"code":"IP_RELEASE_FAILED","message":"IP address release failed during profile switch; switch proceeded"}}` |

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
| `RESOURCE_LIMIT_EXCEEDED` | 422 | Legacy alias — superseded by `TENANT_LIMIT_EXCEEDED` (retained for backward compatibility on older paths) | `{"status":"error","error":{"code":"RESOURCE_LIMIT_EXCEEDED","message":"Tenant resource limit exceeded","details":[{"resource":"sims","current":1000000,"limit":1000000}]}}` |
| `TENANT_LIMIT_EXCEEDED` | 422 | Tenant has reached a resource limit (max SIMs, APNs, users, api_keys). Enforced by tenant-limits middleware (STORY-068 AC-8) | `{"status":"error","error":{"code":"TENANT_LIMIT_EXCEEDED","message":"Tenant resource limit exceeded","details":[{"resource":"api_keys","current":20,"max":20}]}}` |
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
| `SERVICE_DEGRADED` | 503 | A kill-switch is active that blocks the requested operation (e.g., `bulk_operations` kill-switch blocks all bulk SIM endpoints). The specific kill-switch key is logged server-side but not exposed in the response. | `{"status":"error","error":{"code":"SERVICE_DEGRADED","message":"This operation is currently disabled. Please try again later."}}` |

---

## Error Code Constants (Go)

All error codes are defined as constants in a single file for easy reference:

```go
package errors

const (
    // Auth
    CodeInvalidCredentials       = "INVALID_CREDENTIALS"
    CodeAccountLocked            = "ACCOUNT_LOCKED"
    CodeAccountDisabled          = "ACCOUNT_DISABLED"
    CodeInvalid2FACode           = "INVALID_2FA_CODE"
    CodeTokenExpired             = "TOKEN_EXPIRED"
    CodeInvalidRefreshToken      = "INVALID_REFRESH_TOKEN"
    CodePasswordChangeRequired   = "PASSWORD_CHANGE_REQUIRED"
    CodePasswordTooShort         = "PASSWORD_TOO_SHORT"
    CodePasswordMissingClass     = "PASSWORD_MISSING_CLASS"
    CodePasswordRepeatingChars   = "PASSWORD_REPEATING_CHARS"
    CodePasswordReused           = "PASSWORD_REUSED"
    CodeAPIKeyIPNotAllowed       = "API_KEY_IP_NOT_ALLOWED"

    // Authorization
    CodeForbidden        = "FORBIDDEN"
    CodeInsufficientRole = "INSUFFICIENT_ROLE"
    CodeScopeDenied      = "SCOPE_DENIED"

    // Validation
    CodeValidationError  = "VALIDATION_ERROR"
    CodeInvalidFormat    = "INVALID_FORMAT"
    CodeInvalidReference  = "INVALID_REFERENCE"  // FIX-206 (FK violation translation)
    CodeInvalidIMSIFormat = "INVALID_IMSI_FORMAT" // FIX-207 (malformed IMSI rejected at API/AAA)
    CodeInvalidSeverity   = "INVALID_SEVERITY"    // FIX-211 (non-canonical severity value)
    CodeInvalidParam      = "INVALID_PARAM"        // FIX-233 (query param out of range/semantics)

    // Resource
    CodeNotFound      = "NOT_FOUND"
    CodeAlreadyExists = "ALREADY_EXISTS"
    CodeConflict      = "CONFLICT"
    CodeAlertNotFound        = "ALERT_NOT_FOUND"        // FIX-209 (unified alerts table)
    CodeAlertNoData          = "ALERT_NO_DATA"          // FIX-229 (export empty result)
    CodeSuppressionNotFound  = "SUPPRESSION_NOT_FOUND"  // FIX-229 (suppression delete/get)
    CodeDuplicate            = "DUPLICATE"              // FIX-229 (duplicate rule_name on CREATE)

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
    CodeOperatorNotFound       = "OPERATOR_NOT_FOUND"
    CodeOperatorDown           = "OPERATOR_DOWN"
    CodeCircuitOpen            = "CIRCUIT_OPEN"
    CodeFailoverExhausted      = "FAILOVER_EXHAUSTED"
    CodeProtocolNotConfigured  = "PROTOCOL_NOT_CONFIGURED"  // STORY-090 Gate F-A3 (VAL-030)

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
    CodeNotESIM              = "NOT_ESIM"
    CodeInvalidProfileState  = "INVALID_PROFILE_STATE"
    CodeSameProfile          = "SAME_PROFILE"
    CodeDifferentSIM         = "DIFFERENT_SIM"
    CodeSMDPPlusError        = "SM_DP_PLUS_ERROR"
    CodeSwitchFailed         = "SWITCH_FAILED"
    CodeProfileLimitExceeded = "PROFILE_LIMIT_EXCEEDED"
    CodeCannotDeleteEnabled  = "CANNOT_DELETE_ENABLED_PROFILE"
    CodeDuplicateProfile     = "DUPLICATE_PROFILE"
    CodeProfileNotAvailable  = "PROFILE_NOT_AVAILABLE"
    CodeIPReleaseFailed      = "IP_RELEASE_FAILED"

    // Job
    CodeJobNotFound       = "JOB_NOT_FOUND"
    CodeJobAlreadyRunning = "JOB_ALREADY_RUNNING"
    CodeJobCancelled      = "JOB_CANCELLED"

    // Tenant
    CodeResourceLimitExceeded = "RESOURCE_LIMIT_EXCEEDED"  // legacy alias
    CodeTenantLimitExceeded   = "TENANT_LIMIT_EXCEEDED"
    CodeTenantSuspended       = "TENANT_SUSPENDED"

    // Rate Limit
    CodeRateLimited = "RATE_LIMITED"

    // System
    CodeInternalError       = "INTERNAL_ERROR"
    CodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
    CodeDatabaseError       = "DATABASE_ERROR"
    CodeServiceDegraded     = "SERVICE_DEGRADED"
)
```

---

## Severity Taxonomy

Argus uses a single canonical 5-value severity enum across alerts, anomalies, policy violations, notifications, and notification preferences. Defined in Go as `internal/severity/severity.go` and mirrored in the frontend as `web/src/lib/severity.ts` (exports `SEVERITY_VALUES`, `SEVERITY_OPTIONS`, `SEVERITY_FILTER_OPTIONS`, `severityOrdinal`). Established in FIX-211.

### Values

Strictly ordered from lowest urgency to highest: `info < low < medium < high < critical`.

| Severity | Ordinal | Meaning |
|----------|---------|---------|
| `info` | 1 | Operational information; no action needed |
| `low` | 2 | Cosmetic / minor; batched review |
| `medium` | 3 | Attention needed within 24h (replaces legacy `warning`) |
| `high` | 4 | Active issue, respond within 1h (replaces legacy `error`) |
| `critical` | 5 | Page on-call immediately |

### Legacy value migration

Historical rows used a 4-tier `info/warning/error/critical` taxonomy. The FIX-211 migration (`20260421000003_severity_taxonomy_unification`) remaps:

| Old value | New value |
|-----------|-----------|
| `critical` | `critical` (unchanged) |
| `error` | `high` |
| `warning` | `medium` |
| `info` | `info` (unchanged) |

### Colour coding (frontend)

The `<SeverityBadge>` component in `web/src/components/shared/severity-badge.tsx` renders each severity with these tokens (defined in `docs/FRONTEND.md`):

| Severity | Background | Foreground |
|----------|------------|------------|
| `critical` | `bg-danger-dim` | `text-danger` (pulse) |
| `high` | `bg-danger-dim` | `text-danger` (static) |
| `medium` | `bg-warning-dim` | `text-warning` |
| `low` | `bg-info/10` | `text-info` |
| `info` | `bg-bg-elevated` | `text-text-secondary` |

No hardcoded hex; all colour derives from design tokens.

### Consumers

**DB tables (all carry CHECK constraint enforcing the 5 values):**
- `anomalies.severity`
- `policy_violations.severity`
- `notifications.severity`
- `notification_preferences.severity_threshold`
- `alerts.severity` — reserved for FIX-209 when the unified `alerts` table is introduced. FIX-209 MUST adopt the same 5-value CHECK constraint.

**API surfaces that accept severity (validated with HTTP 400 `INVALID_SEVERITY` on non-canonical input):**
- `GET /alerts?severity=...`
- `GET /anomalies?severity=...`
- `GET /violations?severity=...`
- `GET /ops/incidents?severity=...`
- `PATCH /notifications/preferences { severity_threshold }`

**Backend event publishers (emit only canonical values):**
- Policy enforcer (`internal/policy/enforcer/`)
- Operator health checker (`internal/operator/health.go`) — SLA breaches emit `high`; operator-down emits `critical`; operator-up emits `info`
- Bus consumer-lag alerts (`internal/bus/consumer_lag.go`)
- System revoke-sessions notifications (`internal/api/system/revoke_sessions_handler.go`)
- Import job notifications (`internal/job/import.go`)

### Validation policy

**Strict (no toggle).** Every severity-accepting endpoint rejects non-canonical values with HTTP 400 `INVALID_SEVERITY`. There is no `SEVERITY_STRICT_VALIDATION` flag; Argus is single-tenant and all consumers are in-repo. Seed data in `migrations/seed/003_comprehensive_seed.sql` uses canonical values exclusively — `make db-seed` on a fresh volume is guaranteed clean.

### Cross-reference for FIX-209

FIX-209 introduces the unified `alerts` table. Its `severity` column MUST carry:

```sql
CHECK (severity IN ('critical','high','medium','low','info'))
```

Constraint name: `chk_alerts_severity`. Do NOT reintroduce `warning` or `error`.

---

## Alerts Taxonomy

Argus persists every `argus.events.alert.triggered` NATS event into a single `alerts` table. The table is the canonical source of truth for operator, infrastructure, policy, and SIM-level alert history. SIM-level alerts also retain their anomaly row (see `anomalies` table) linked via `alerts.meta.anomaly_id`. Established in FIX-209.

**Source:** `internal/store/alert.go` · `internal/api/alert/handler.go` · `internal/notification/service.go` (`handleAlertPersist` + `parseAlertPayload`).

### Severity

Uses the canonical 5-value enum from FIX-211 (see [§Severity Taxonomy](#severity-taxonomy)). Constraint: `chk_alerts_severity CHECK (severity IN ('critical','high','medium','low','info'))`.

### State

| State | Meaning | Transitions |
|---|---|---|
| `open` | New, unacknowledged | → `acknowledged`, `resolved`, `suppressed` |
| `acknowledged` | Operator has seen / owns it | → `resolved`, `suppressed` |
| `suppressed` | Dedup/cooldown suppression — managed by `SuppressAlert`/`UnsuppressAlert` internally or by admin action. NOT settable via `PATCH /alerts/{id}`. | → `open`, `resolved` |
| `resolved` | Fixed or no longer actionable | terminal |

Transitions via `PATCH /alerts/{id}` (API contract): `open → acknowledged`, `open/acknowledged → resolved` only. `suppressed` transitions are managed exclusively by the dedup state machine (`SuppressAlert`/`UnsuppressAlert` store methods).

Constraint: `chk_alerts_state CHECK (state IN ('open','acknowledged','resolved','suppressed'))`.

**Reserved for FIX-298**: `delivery_failed` — NOT yet in the CHECK. A future story will add it when notification delivery-failure persistence lands.

### Source

| Source | Meaning |
|---|---|
| `sim` | SIM-level detection (anomalies engine + batch). Links back to `anomalies.id` via `alerts.meta.anomaly_id`. |
| `operator` | Mobile operator health, SLA, roaming — from operator/health.go + job/roaming_renewal.go. |
| `infra` | Infrastructure: NATS consumer lag, storage monitor, anomaly-batch crash. |
| `policy` | Policy engine violations. |
| `system` | Fallback for unknown `alert_type` values (log-warn on persist). |

Constraint: `chk_alerts_source CHECK (source IN ('sim','operator','infra','policy','system'))`.

### Publisher → Source map

The notification subscriber `parseAlertPayload` resolves `source` from `alert_type` via this table. Rows are added here when a new publisher ships.

| Alert type | Source | Publisher file |
|---|---|---|
| `anomaly_sim_cloning` / `anomaly_data_spike` / `anomaly_auth_flood` / `anomaly_nas_flood` / `anomaly_velocity` / `anomaly_location` | `sim` | `internal/analytics/anomaly/engine.go`, `batch.go` |
| `operator_down`, `operator_recovered`, `sla_violation` | `operator` | `internal/operator/health.go` |
| `roaming.agreement.renewal_due` | `operator` | `internal/job/roaming_renewal.go` |
| `nats_consumer_lag` | `infra` | `internal/bus/consumer_lag.go` |
| `storage.*` (prefix match) | `infra` | `internal/job/storage_monitor.go` |
| `anomaly_batch_crash` | `infra` | `internal/job/anomaly_batch_supervisor.go` |
| `policy_violation` | `policy` | `internal/policy/enforcer/enforcer.go` |
| (any other) | `system` | — |

### Publisher payload tolerance

FIX-209 persists alerts tolerantly: publisher payload shapes differ across the 7 sites. Until FIX-212 normalizes the envelope, the subscriber accepts a flexible struct with both `alert_type`/`type`, `title`/`message`, `timestamp`/`detected_at`, `metadata`/`details` field aliases, and synthesizes missing titles/descriptions. Publishers with NO `tenant_id` in their payload today (e.g. `nats_consumer_lag`, `anomaly_batch_crash`, `storage_monitor` explicit-nil, `operator.AlertEvent`, `notification.AlertPayload`) currently skip persist but STILL dispatch notifications — FIX-212 closes that gap.

### Dedup & Cooldown

Introduced by FIX-210. Every incoming alert event is assigned a `dedup_key` computed as:

```
SHA-256( tenant_id | type | source | entity_triple )
```

where `entity_triple` is one of `sim:<uuid>`, `op:<uuid>`, `apn:<uuid>`, or `-` (no entity).

**Why severity is excluded from the hash (Decision D3):** Dedup identity represents root cause, not measurement intensity. Two events for the same operator/type pair that differ only in severity are the same underlying incident (the severity of an open alert escalates-only on dedup hit; downgrades are ignored).

**Upsert mechanics (`UpsertWithDedup`):**

Uses an atomic `INSERT ... ON CONFLICT (tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed') DO UPDATE`. The partial unique index is `idx_alerts_dedup_unique`. On conflict:
- `occurrence_count += 1`
- `last_seen_at = NOW()`
- `severity` escalates only (higher ordinal wins; downgrade ignored)
- `fired_at` and `first_seen_at` NEVER change after INSERT (stable cursor pagination)

**Cooldown:**

On resolve, `cooldown_until = NOW() + ALERT_COOLDOWN_MINUTES`. New events matching the `dedup_key` within the cooldown window return `UpsertCoolingDown` from the store. The Prometheus counter `argus_alerts_cooldown_dropped_total` increments; notification dispatch still runs.

A second partial index `idx_alerts_cooldown_lookup` covers resolved rows with a non-null `cooldown_until` for efficient cooldown checks.

**Edge-triggering:**

In-scope publishers (operator health checker, policy enforcer) only publish on state transitions or when min-interval elapses. The operator health worker persists `previous_state` per entity in Redis to survive restarts.

### Retention

Controlled by `ALERTS_RETENTION_DAYS` (default 180, min 30) — see [CONFIG.md](CONFIG.md). Daily cron job `alerts_retention` at 03:15 UTC calls `AlertStore.DeleteOlderThan(now - retention_days)`.

### Cross-reference for FIX-210 — SHIPPED 2026-04-21

FIX-210 (Alert Deduplication + State Machine) is fully shipped. Delivered:
- `dedup_key VARCHAR(255) NOT NULL` column populated at persist time via SHA-256 hash.
- Partial unique index `idx_alerts_dedup_unique` on `(tenant_id, dedup_key) WHERE state IN ('open','acknowledged','suppressed')`.
- Partial lookup index `idx_alerts_cooldown_lookup` on resolved rows with non-null `cooldown_until`.
- `UpsertWithDedup` store method (atomic upsert, occurrence increment, severity escalation).
- `SuppressAlert` / `UnsuppressAlert` store methods (not exposed via public PATCH API).
- Cooldown logic gated by `ALERT_COOLDOWN_MINUTES` (default 5, range 0–1440).
- Edge-triggered publishers: operator health checker + policy enforcer.
- Prometheus counters: `argus_alerts_deduplicated_total{type}`, `argus_alerts_cooldown_dropped_total`.
- `suppressed` state is now actively used (previously reserved).

---

## Client Error Handling Guidelines

1. **Always check `error.code`** for programmatic handling, never parse `error.message`.
2. **Display `error.message`** to users as-is; it is safe and localization-ready.
3. **On 401 TOKEN_EXPIRED**: silently attempt token refresh via `POST /api/v1/auth/refresh`. On refresh failure, redirect to login.
4. **On 401 INVALID_REFRESH_TOKEN**: clear all tokens, redirect to login.
5. **On 429 RATE_LIMITED**: read `Retry-After` header, implement exponential backoff.
6. **On 422 VALIDATION_ERROR**: map `details[].field` to form fields for inline error display.
7. **On 500/503**: show generic error with the correlation_id from the message for support reference.
8. **On 409 CONFLICT**: refetch the resource and retry or prompt the user.
