# Bulk SIM Actions — API Reference

> Three asynchronous endpoints that enqueue background jobs for large-scale SIM operations.
> All three accept a **dual-shape** request body: either an explicit list of SIM IDs (`sim_ids`) or a saved-segment reference (`segment_id`). Exactly one shape must be supplied per call.

Cross-refs: [MIDDLEWARE.md](../MIDDLEWARE.md#notes) · [ERROR_CODES.md](../ERROR_CODES.md)

---

## Common Characteristics

| Property | Value |
|----------|-------|
| Content-Type | `application/json` |
| Authentication | JWT Bearer token |
| Response envelope | Standard `{ status, data, meta?, error? }` via `apierr.WriteSuccess` / `apierr.WriteError` |
| Async pattern | Returns `202 Accepted` immediately; actual work runs as a background job |
| Rate limit | `BulkRateLimiter` — **1 request/second per tenant (burst 2)** (see [rate-limit note](#rate-limit)) |
| Kill-switch | All three are blocked when the `bulk_operations` kill-switch is active → `503 SERVICE_DEGRADED` |
| Max SIMs per call | 10 000 (`sim_ids` array) |

### Dual-Shape Request

Every bulk endpoint accepts two mutually exclusive input shapes:

- **New shape** — explicit list: `{ "sim_ids": ["uuid", ...], ... }`
- **Legacy shape** — segment reference: `{ "segment_id": "uuid", ... }`

Supplying both, or neither, returns `400 VALIDATION_ERROR` (dual-shape violation).

### Rate Limit

All three bulk endpoints are gated by the `BulkRateLimiter` middleware (`internal/gateway/bulk_ratelimit.go`), wired via `r.With(bulkRL)` — **1 req/sec per tenant with burst 2**.
This is a stricter per-endpoint limit that sits on top of the global per-minute tenant rate limit.
On breach: `429 RATE_LIMITED` with `Retry-After` header.

---

## Endpoints

### POST /api/v1/sims/bulk/state-change

Change the lifecycle state of a set of SIMs in a single asynchronous job.

**RBAC:** `sim_manager+` (sim_manager, operator_manager, tenant_admin, super_admin)

#### Request — new shape

```json
{
  "sim_ids": [
    "00000000-0000-0000-0000-000000000001",
    "00000000-0000-0000-0000-000000000002"
  ],
  "target_state": "suspended",
  "reason": "planned maintenance window"
}
```

#### Request — legacy shape

```json
{
  "segment_id": "00000000-0000-0000-0000-000000000010",
  "target_state": "suspended",
  "reason": "planned maintenance window"
}
```

**`target_state` valid values:** `active` | `suspended` | `terminated` | `stolen_lost`

#### Success Response — 202 Accepted

```json
{
  "status": "success",
  "data": {
    "job_id": "00000000-0000-0000-0000-000000000099",
    "total_sims": 250,
    "status": "queued"
  }
}
```

#### Error Responses

| HTTP | Code | When |
|------|------|------|
| 400 | `INVALID_FORMAT` | Request body is not valid JSON or wrong Content-Type |
| 400 | `VALIDATION_ERROR` | Both shapes supplied / neither supplied / empty `sim_ids` array / `sim_ids` count > 10 000 / invalid UUID in array. Array errors include `details.offending_indices`. |
| 403 | `FORBIDDEN_CROSS_TENANT` | One or more `sim_ids` belong to a different tenant. Details include `violations` list. |
| 429 | `RATE_LIMITED` | More than 1 request/sec from the same tenant |
| 503 | `SERVICE_DEGRADED` | `bulk_operations` kill-switch is active |

#### Sample curl — new shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/state-change \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sim_ids": [
      "00000000-0000-0000-0000-000000000001",
      "00000000-0000-0000-0000-000000000002"
    ],
    "target_state": "suspended",
    "reason": "planned maintenance window"
  }'
```

#### Sample curl — legacy shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/state-change \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "segment_id": "00000000-0000-0000-0000-000000000010",
    "target_state": "suspended",
    "reason": "planned maintenance window"
  }'
```

---

### POST /api/v1/sims/bulk/policy-assign

Assign a policy version to a set of SIMs. After the job completes, a Change-of-Authorization (CoA) is dispatched per SIM against any active AAA sessions to enforce the newly-assigned policy.

**RBAC:** `policy_editor+` (policy_editor, operator_manager, tenant_admin, super_admin)

> **CoA note:** CoA is dispatched outside the distributed lock after each SIM is updated. Job result includes `coa_sent_count`, `coa_acked_count`, `coa_failed_count` (omitted when 0).

#### Request — new shape

```json
{
  "sim_ids": [
    "00000000-0000-0000-0000-000000000001",
    "00000000-0000-0000-0000-000000000002"
  ],
  "policy_version_id": "00000000-0000-0000-0000-000000000020",
  "reason": "migrate to fair-use policy v3"
}
```

#### Request — legacy shape

```json
{
  "segment_id": "00000000-0000-0000-0000-000000000010",
  "policy_version_id": "00000000-0000-0000-0000-000000000020",
  "reason": "migrate to fair-use policy v3"
}
```

#### Success Response — 202 Accepted

```json
{
  "status": "success",
  "data": {
    "job_id": "00000000-0000-0000-0000-000000000099",
    "total_sims": 1500,
    "status": "queued"
  }
}
```

#### Error Responses

Same shape as state-change, plus:

| HTTP | Code | When |
|------|------|------|
| 400 | `INVALID_FORMAT` | Request body is not valid JSON or wrong Content-Type |
| 400 | `VALIDATION_ERROR` | Dual-shape violation / empty array / count > 10 000 / bad UUID / `policy_version_id` missing or invalid |
| 403 | `FORBIDDEN_CROSS_TENANT` | One or more `sim_ids` belong to a different tenant |
| 429 | `RATE_LIMITED` | More than 1 request/sec from the same tenant |
| 503 | `SERVICE_DEGRADED` | `bulk_operations` kill-switch is active |

#### Sample curl — new shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/policy-assign \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sim_ids": [
      "00000000-0000-0000-0000-000000000001",
      "00000000-0000-0000-0000-000000000002"
    ],
    "policy_version_id": "00000000-0000-0000-0000-000000000020",
    "reason": "migrate to fair-use policy v3"
  }'
```

#### Sample curl — legacy shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/policy-assign \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "segment_id": "00000000-0000-0000-0000-000000000010",
    "policy_version_id": "00000000-0000-0000-0000-000000000020",
    "reason": "migrate to fair-use policy v3"
  }'
```

---

### POST /api/v1/sims/bulk/operator-switch

Switch a set of SIMs to a different operator and APN. **Only eSIM rows are switched.** Non-eSIM SIMs included in the input are not processed and appear in the job's `error_report` with code `NOT_ESIM`.

**RBAC:** `tenant_admin` (tenant_admin, super_admin)

#### Request — new shape

```json
{
  "sim_ids": [
    "00000000-0000-0000-0000-000000000001",
    "00000000-0000-0000-0000-000000000002"
  ],
  "target_operator_id": "00000000-0000-0000-0000-000000000030",
  "target_apn_id": "00000000-0000-0000-0000-000000000040",
  "reason": "migrate to primary operator"
}
```

#### Request — legacy shape

```json
{
  "segment_id": "00000000-0000-0000-0000-000000000010",
  "target_operator_id": "00000000-0000-0000-0000-000000000030",
  "target_apn_id": "00000000-0000-0000-0000-000000000040",
  "reason": "migrate to primary operator"
}
```

#### Success Response — 202 Accepted

```json
{
  "status": "success",
  "data": {
    "job_id": "00000000-0000-0000-0000-000000000099",
    "total_sims": 500,
    "status": "queued"
  }
}
```

#### Error Responses

| HTTP | Code | When |
|------|------|------|
| 400 | `INVALID_FORMAT` | Request body is not valid JSON or wrong Content-Type |
| 400 | `VALIDATION_ERROR` | Dual-shape violation / empty array / count > 10 000 / bad UUID / `target_operator_id` or `target_apn_id` missing |
| 403 | `FORBIDDEN_CROSS_TENANT` | One or more `sim_ids` belong to a different tenant |
| 429 | `RATE_LIMITED` | More than 1 request/sec from the same tenant |
| 503 | `SERVICE_DEGRADED` | `bulk_operations` kill-switch is active |

#### Sample curl — new shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/operator-switch \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "sim_ids": [
      "00000000-0000-0000-0000-000000000001",
      "00000000-0000-0000-0000-000000000002"
    ],
    "target_operator_id": "00000000-0000-0000-0000-000000000030",
    "target_apn_id": "00000000-0000-0000-0000-000000000040",
    "reason": "migrate to primary operator"
  }'
```

#### Sample curl — legacy shape

```bash
curl -s -X POST https://localhost:8080/api/v1/sims/bulk/operator-switch \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "segment_id": "00000000-0000-0000-0000-000000000010",
    "target_operator_id": "00000000-0000-0000-0000-000000000030",
    "target_apn_id": "00000000-0000-0000-0000-000000000040",
    "reason": "migrate to primary operator"
  }'
```

---

## Error Catalog

Summary of all error codes that can be returned by the three bulk endpoints.

| Code | HTTP | When it fires | Detail shape |
|------|------|---------------|--------------|
| `INVALID_FORMAT` | 400 | Request body is not valid JSON, Content-Type is not `application/json`, or body is empty | None |
| `VALIDATION_ERROR` | 400 | Dual-shape violation (both or neither of `sim_ids`/`segment_id`); empty `sim_ids` array; array length > 10 000; invalid UUID in `sim_ids`; missing required field | `{"offending_indices": [3, 7]}` for array UUID errors; field-level errors otherwise |
| `FORBIDDEN_CROSS_TENANT` | 403 | One or more SIM IDs in `sim_ids` are owned by a different tenant | `{"violations": ["uuid-a", "uuid-b"]}` |
| `RATE_LIMITED` | 429 | More than 1 request/second from the same tenant on any bulk endpoint | `{"limit": 1, "window": "per_second", "retry_after_seconds": 1}` + `Retry-After` header |
| `SERVICE_DEGRADED` | 503 | The `bulk_operations` kill-switch is toggled active in the admin kill-switch table | None |

Full error code specifications: [ERROR_CODES.md](../ERROR_CODES.md)

---

## Example 403 Response — Cross-Tenant Violation

When one or more SIM IDs in the request do not belong to the caller's tenant, the entire request is rejected before the job is enqueued:

```json
{
  "status": "error",
  "error": {
    "code": "FORBIDDEN_CROSS_TENANT",
    "message": "One or more SIM IDs belong to a different tenant",
    "details": {
      "violations": [
        "00000000-0000-0000-0000-000000000099",
        "00000000-0000-0000-0000-000000000098"
      ]
    }
  }
}
```

---

## Example VALIDATION_ERROR Response — Array UUID Errors

When specific indices in `sim_ids` contain invalid UUIDs:

```json
{
  "status": "error",
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": {
      "offending_indices": [3, 7]
    }
  }
}
```
