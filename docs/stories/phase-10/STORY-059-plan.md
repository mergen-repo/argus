# Implementation Plan: STORY-059 — Security & Compliance Hardening

## Goal

Close all deferred security items (encrypted TOTP, NATS tenant isolation, salted pseudonymization, PDF exports, webhook validation, IPv6, stolen_lost transition, rate-limiter wiring, cookie hardening) so Argus passes production security review with zero open deferrals.

## Architecture Context

### Components Involved

| Component | Layer | Responsibility | File Path Pattern |
|-----------|-------|---------------|-------------------|
| SVC-01 Gateway | HTTP/middleware | Cookie flags, CORS, security headers | `internal/gateway/` |
| SVC-02 WebSocket | WS relay | NATS→WS tenant isolation | `internal/ws/hub.go` |
| SVC-03 Core API | API handlers | Compliance PDF export, webhook validation, SIM state | `internal/api/compliance/`, `internal/api/sim/` |
| SVC-07 Analytics | Anomaly detection | IPv6 extractIP fix | `internal/analytics/anomaly/detector.go` |
| SVC-08 Notification | Multi-channel dispatch | Webhook validation, rate limiter wiring | `internal/notification/` |
| SVC-10 Audit/Compliance | Compliance reporting | Pseudonymization unification, PDF export | `internal/compliance/`, `internal/store/compliance.go` |
| Auth | Authentication | TOTP secret encryption | `internal/auth/totp.go`, `internal/store/user.go` |
| Crypto | Encryption | AES-256-GCM encrypt/decrypt | `internal/crypto/aes.go` |
| Config | Env vars | New config vars | `internal/config/config.go` |
| Store | Data access | Session tenant filter, user TOTP | `internal/store/session_radius.go`, `internal/store/user.go`, `internal/store/audit.go` |
| Bus | NATS event publishers | Tenant-ID injection in payloads | `internal/bus/`, all publishers |
| Frontend | React SPA | Webhook validation mirror, PDF toggle, stolen_lost color | `web/src/` |

### Data Flow

**NATS Event → WebSocket (AC-9):**
```
Publisher (any service) → eventBus.Publish(subject, payload{tenant_id: X})
  → NATS JetStream
  → Hub.relayNATSEvent(subject, data)
    → extract tenant_id from payload
    → if tenant_id == nil → BroadcastAll (system event)
    → else → BroadcastToTenant(tenant_id, ...)
```

**TOTP Encryption (AC-7):**
```
GenerateTOTPSecret() → plaintext secret
  → crypto.Encrypt(secret, ENCRYPTION_KEY) → ciphertext
  → store in users.totp_secret (encrypted)

ValidateTOTPCode(userID, code):
  → read users.totp_secret (encrypted)
  → crypto.Decrypt(ciphertext, ENCRYPTION_KEY) → plaintext
  → totp.Validate(code, plaintext)
```

**PDF Export (AC-8):**
```
GET /api/v1/compliance/reports/:id/export?format=pdf
  → ComplianceHandler.BTKReport
  → if format=pdf: complianceSvc.ExportBTKReportPDF()
    → generate PDF using go-pdf/fpdf
    → return PDF bytes with Content-Type: application/pdf
```

### API Specifications

**`GET /api/v1/compliance/reports/:id/export?format=pdf`** (AC-8 — extends existing BTK endpoint)
- Query params: `format=json|csv|pdf`
- Success: PDF binary with `Content-Type: application/pdf`, `Content-Disposition: attachment`
- Error: `{ status: "error", error: { code: "INTERNAL_ERROR", message: "..." } }`
- Status codes: 200, 400, 401, 403, 500

**Standard envelope for all other modifications:** existing endpoints, no new routes.

### Database Schema

**Source: `migrations/20260320000002_core_schema.up.sql` (ACTUAL)**

Users table (relevant columns):
```sql
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    email VARCHAR(255) NOT NULL,
    password_hash VARCHAR(255) NOT NULL DEFAULT '',
    name VARCHAR(100) NOT NULL,
    role VARCHAR(30) NOT NULL,
    totp_secret VARCHAR(255),        -- AC-7: currently plaintext, will become ciphertext
    totp_enabled BOOLEAN NOT NULL DEFAULT false,
    state VARCHAR(20) NOT NULL DEFAULT 'active',
    last_login_at TIMESTAMP,
    failed_login_count INTEGER NOT NULL DEFAULT 0,
    locked_until TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
```

**Migration needed (AC-7):** No schema change required. Column `totp_secret VARCHAR(255)` is wide enough for base64-encoded ciphertext. Re-encryption of existing rows handled at application level (Go startup migration function), not SQL migration, because AES-256-GCM requires Go crypto library.

**SIM state transitions (AC-6):**
```
validTransitions map in internal/store/sim.go:
  "stolen_lost": {}                 → change to: {"terminated"}
```

Sessions table — no schema change, just query filter addition (AC-2).

### Screen Mockups

Minimal UI in this story. Affected screens:

**SCR-125 Compliance Reports** — PDF export toggle:
- Add format selector (dropdown or toggle) next to existing CSV download button
- Options: JSON, CSV, PDF

**SCR-110 Notification Channels** — webhook validation mirror:
- URL field: validate HTTPS scheme, non-empty
- Secret field: validate non-empty
- Frontend mirrors backend validation before submit

No new screens created.

### Design Token Map (UI stories ONLY — minimal UI)

#### Color Tokens
| Usage | Token Class | NEVER Use |
|-------|-------------|-----------|
| stolen_lost badge bg | `bg-[var(--danger-dim)]` | `bg-red-100`, `bg-[#ffe0e6]` |
| stolen_lost badge text | `text-[var(--danger)]` | `text-red-500`, `text-[#ff4466]` |
| Primary text | `text-[var(--text-primary)]` | `text-gray-900`, `text-white` |
| Secondary text | `text-[var(--text-secondary)]` | `text-gray-500` |
| Accent button | `bg-[var(--accent)]` | `bg-blue-500` |
| Card bg | `bg-[var(--bg-surface)]` | `bg-white`, `bg-gray-800` |
| Border | `border-[var(--border)]` | `border-gray-200` |

#### Existing Components to REUSE
| Component | Path | Use For |
|-----------|------|---------|
| `<Button>` | `web/src/components/atoms/Button.tsx` | PDF download button |
| `<Input>` | `web/src/components/atoms/Input.tsx` | Webhook URL/secret fields |
| `<Badge>` | `web/src/components/atoms/Badge.tsx` | SIM state badges |
| `<Select>` | `web/src/components/atoms/Select.tsx` | Format selector |

## Prerequisites

- [x] STORY-056 completed (runtime fixes unblock testing)
- [x] `internal/crypto/aes.go` exists with Encrypt/Decrypt/EncryptJSON/DecryptJSON functions
- [x] `ENCRYPTION_KEY` env var already used for operator secrets
- [x] `internal/notification/delivery.go` has `RateLimiter` interface defined

## Tasks

### Task 1: IPv6 extractIP fix + session tenant filter (AC-1, AC-2)

- **Files:** Modify `internal/analytics/anomaly/detector.go`, Modify `internal/store/session_radius.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/analytics/anomaly/detector.go` — current `extractIP` function at line 237; Read `internal/store/session_radius.go` — `GetLastSessionBySIM` at line 537
- **Context refs:** Architecture Context > Components Involved, Acceptance Criteria AC-1 AC-2, Test Scenarios
- **What:**
  1. **AC-1:** Replace `extractIP()` (line 237) with proper IPv6 handling. Current implementation splits on last colon — fails for IPv6 like `[2001:db8::1]:4500`. Use `net.SplitHostPort` first (handles bracketed IPv6), fall back to `net.ParseIP` for bare addresses. Add import for `"net"`.
  2. **AC-2:** In `GetLastSessionBySIM` (line 537), add `AND tenant_id = $2` to the query. Change function signature to accept `tenantID uuid.UUID` as second parameter: `GetLastSessionBySIM(ctx context.Context, tenantID, simID uuid.UUID)`. Update all callers.
  3. Add unit tests: `extractIP("[2001:db8::1]:4500")` returns `"2001:db8::1"`, `extractIP("192.168.1.1:4500")` still works, `extractIP("bare-ip")` returns `"bare-ip"`.
  4. Add unit test: `GetLastSessionBySIM` with mismatched tenant returns nil.
- **Verify:** `go test ./internal/analytics/anomaly/ ./internal/store/ -run "TestExtractIP|TestGetLastSessionBySIM" -v`

### Task 2: Pseudonymization unification (AC-3)

- **Files:** Modify `internal/store/audit.go`, Modify `internal/compliance/service.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/compliance.go` — `hashWithSalt` function at line 422 and `anonymizeJSONWithSalt` at line 427
- **Context refs:** Architecture Context > Components Involved, Pseudonymization Details, Acceptance Criteria AC-3, Test Scenarios
- **What:**
  1. **The problem:** `RightToErasure` in `compliance/service.go:126` calls `auditStore.Pseudonymize()` which uses UNSALTED SHA-256 (see `audit.go:305` — `sha256.Sum256([]byte(strVal))`). But `RunPurgeSweep` uses `complianceStore.PseudonymizeAuditLogs()` which uses SALTED SHA-256 via `hashWithSalt(strVal, salt)`. Same identifier produces different hashes depending on which path runs — breaks compliance.
  2. **Fix:** Extract `deriveTenantSalt()` from `compliance/service.go:279` into a shared package (`internal/compliance/salt.go` or keep in `compliance/service.go` and export it). Modify `AuditStore.Pseudonymize()` to accept a `tenantSalt string` parameter and use `anonymizeJSONWithSalt` (from `store/compliance.go`) instead of `anonymizeJSON`. Delete the unsalted `anonymizeJSON` function from `audit.go`.
  3. Update the caller in `compliance/service.go:126` to pass `tenantSalt`: `s.auditStore.Pseudonymize(ctx, tenantID, entityIDs, tenantSalt)`.
  4. Add unit test: verify `RightToErasure` pseudonym output matches `RunPurgeSweep` pseudonym for same tenant+identifier.
- **Verify:** `go test ./internal/compliance/ ./internal/store/ -run "TestPseudonymUnified|TestRightToErasure" -v`

### Pseudonymization Details

Current state:
- `compliance/service.go:279` has `deriveTenantSalt(tenantID)` — generates `sha256("argus-compliance-salt:" + tenantID)[:16]`
- `store/compliance.go:422` has `hashWithSalt(value, salt)` — `sha256(salt + "|" + value)`
- `store/compliance.go:427` has `anonymizeJSONWithSalt(data, fields, salt)` — uses `hashWithSalt`
- `store/audit.go:291` has `anonymizeJSON(data, fields)` — uses UNSALTED `sha256(value)` ← BUG
- `compliance/service.go:94` (RunPurgeSweep) calls `complianceStore.PseudonymizeAuditLogs` with salt ← CORRECT
- `compliance/service.go:126` (RightToErasure) calls `auditStore.Pseudonymize` WITHOUT salt ← BUG

### Task 3: Webhook validation + frontend mirror (AC-4)

- **Files:** Modify `internal/notification/service.go`, Modify `web/src/pages/settings/notifications.tsx`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/webhook.go` — current webhook sender; Read `internal/notification/service.go` — `dispatchToChannels` at line 346
- **Context refs:** Architecture Context > Components Involved, Webhook Validation Details, Design Token Map, Acceptance Criteria AC-4
- **What:**
  1. **Backend:** In `dispatchToChannels` (line 394 — `ChannelWebhook` case), before calling `s.webhook.SendWebhook()`, validate: (a) URL is non-empty and starts with `https://` (use `url.Parse`), (b) secret is non-empty. If validation fails, log structured error and skip dispatch — do NOT attempt partial delivery. Currently the code passes empty strings (`s.webhook.SendWebhook(ctx, "", "", ...)`) which would cause runtime failure.
  2. **Backend:** Add a validation function `ValidateWebhookConfig(url, secret string) error` to `internal/notification/webhook.go` that checks HTTPS scheme and non-empty secret. Use this in both `dispatchToChannels` and any webhook config save endpoint.
  3. **Frontend:** In `web/src/pages/settings/notifications.tsx`, add client-side validation for webhook channel config: URL must start with `https://`, secret must be non-empty. Show validation error inline before submit.
  4. Add unit test: empty webhook URL rejected at dispatch — no partial delivery attempt.
- **Tokens:** Use ONLY classes from Design Token Map — zero hardcoded hex/px
- **Components:** Reuse `<Input>` atom — NEVER raw `<input>`
- **Note:** Invoke `frontend-design` skill for UI changes
- **Verify:** `go test ./internal/notification/ -run "TestWebhookValidation" -v` + visual check of webhook config form

### Webhook Validation Details

Current `dispatchToChannels` in `service.go:394-411`:
```
case ChannelWebhook:
    if s.webhook != nil {
        payload, _ := json.Marshal(...)
        if err := s.webhook.SendWebhook(ctx, "", "", string(payload)); err != nil {
            // ... retry
        }
    }
```
The URL and secret are hardcoded empty strings — the webhook config is not being read from anywhere. This needs to either:
- Accept URL/secret from the notification config (per-tenant webhook settings stored in `notification_configs` table)
- Or at minimum, validate before dispatch and reject empty configs with structured error

### Task 4: Notification rate limiter wiring (AC-5)

- **Files:** Modify `cmd/argus/main.go`, Modify `internal/config/config.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/delivery.go` — `RateLimiter` interface at line 30; Read `internal/gateway/ratelimit.go` — Redis rate limiter pattern
- **Context refs:** Architecture Context > Components Involved, Rate Limiter Wiring Details, Acceptance Criteria AC-5
- **What:**
  1. **Config:** Add `NotificationRateLimitPerMin int` field to `config.Config` with env var `NOTIFICATION_RATE_LIMIT_PER_MINUTE` default `60`.
  2. **Redis rate limiter:** Create `internal/notification/redis_ratelimiter.go` implementing the `RateLimiter` interface using Redis sliding window (ZADD/ZREMRANGEBYSCORE/ZCARD pattern). Similar to gateway rate limiter but simplified for notification use case.
  3. **Wiring:** In `cmd/argus/main.go` line 414, replace `notification.NewDeliveryTracker(nil, log.Logger)` with `notification.NewDeliveryTracker(notifRedisRL, log.Logger)` where `notifRedisRL` is the new Redis rate limiter initialized with `rdb.Client` and `cfg.NotificationRateLimitPerMin`.
  4. Add integration test: 61st notification in 60s window for a tenant is rate-limited.
- **Verify:** `go build ./cmd/argus/` (compile check) + `go test ./internal/notification/ -run "TestRedisRateLimiter" -v`

### Rate Limiter Wiring Details

Current state in `cmd/argus/main.go:414`:
```go
notifDelivery := notification.NewDeliveryTracker(nil, log.Logger)
```
The `nil` means rate limiting is disabled in production.

`RateLimiter` interface (from `delivery.go:30`):
```go
type RateLimiter interface {
    Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}
```

The Redis implementation should use the sliding window pattern (ZADD + ZREMRANGEBYSCORE + ZCARD) similar to `internal/gateway/ratelimit.go` but adapted for the notification `RateLimiter` interface.

### Task 5: SIM stolen_lost → terminated transition + color (AC-6)

- **Files:** Modify `internal/store/sim.go`, Modify `web/src/lib/sim-utils.ts`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/sim.go` — `validTransitions` map at line 86, `ChangeStateTx` around line 700
- **Context refs:** Architecture Context > Components Involved, SIM State Transition Details, Design Token Map, Acceptance Criteria AC-6
- **What:**
  1. **State machine:** In `internal/store/sim.go:90`, change `"stolen_lost": {}` to `"stolen_lost": {"terminated"}`. This allows `stolen_lost → terminated` transition per BR-1 from PRODUCT.md.
  2. **Authorization:** The existing `ChangeStateTx` method handles `terminated` case (sets `purge_at = NOW() + interval`) and IP release. The `stolen_lost → terminated` path flows through the same code — no duplicate logic needed. Verify `sim_manager` role can execute this transition (check RBAC in `internal/api/sim/handler.go`).
  3. **Frontend color:** Two locations to verify:
     - `web/src/lib/sim-utils.ts` — `stateVariant()` already handles `stolen_lost` returning `'danger'`. No change needed.
     - `web/src/pages/dashboard/index.tsx:29` — `STATE_COLORS` map already has `stolen_lost: 'var(--color-purple)'`. This is the actual map referenced by AC-6. Verify it's present — no change needed if already there (it is).
     - The story references `web/src/lib/sim-colors.ts` which does NOT exist — the actual locations are `sim-utils.ts` and `dashboard/index.tsx`. This is a stale reference.
  4. Add integration test: `PATCH /sims/:id {state: "terminated"}` from `stolen_lost` succeeds, history row created.
- **Tokens:** Use ONLY classes from Design Token Map
- **Verify:** `go test ./internal/store/ -run "TestStolenLostToTerminated" -v`

### SIM State Transition Details

Current `validTransitions` (from `internal/store/sim.go:86-93`):
```go
var validTransitions = map[string][]string{
    "ordered":     {"active"},
    "active":      {"suspended", "stolen_lost", "terminated"},
    "suspended":   {"active", "terminated"},
    "stolen_lost": {},              // ← EMPTY — must add "terminated"
    "terminated":  {"purged"},
    "purged":      {},
}
```

BR-1 from PRODUCT.md specifies:
| From | To | Trigger | Authorization |
|------|----|---------|--------------|
| STOLEN/LOST | TERMINATED | Manual terminate after investigation | Tenant Admin |

The `ChangeStateTx` method already handles `case "terminated"` with IP release grace period and `purge_at`. The stolen_lost → terminated path will naturally use this existing logic.

### Task 6: TOTP secret encryption at rest (AC-7)

- **Files:** Modify `internal/auth/totp.go`, Modify `internal/store/user.go`, Create `internal/auth/totp_crypto.go`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/crypto/aes.go` — Encrypt/Decrypt functions for AES-256-GCM pattern; Read `internal/auth/totp.go` — current TOTP functions; Read `internal/store/user.go` — `SetTOTPSecret` at line 113
- **Context refs:** Architecture Context > Components Involved, TOTP Encryption Details, Acceptance Criteria AC-7
- **What:**
  1. **Encryption helpers:** Create `internal/auth/totp_crypto.go` with:
     - `EncryptTOTPSecret(plainSecret, hexKey string) (string, error)` — uses `crypto.Encrypt` from `internal/crypto/aes.go`, returns base64-encoded ciphertext
     - `DecryptTOTPSecret(encryptedSecret, hexKey string) (string, error)` — uses `crypto.Decrypt`, returns plaintext secret
     - If `hexKey` is empty (dev mode), pass through without encryption (matching `EncryptJSON` behavior)
  2. **Store modification:** In `internal/store/user.go`, modify `SetTOTPSecret` to accept encrypted ciphertext (caller encrypts before calling). No store-level change needed — just document that the value is now encrypted.
  3. **Auth flow:** Modify `ValidateTOTPCode` and `ValidateTOTPCodeWithWindow` in `totp.go` to accept encrypted secret + encryption key. They must decrypt before validating. Alternatively, add wrapper functions that decrypt then validate.
  4. **Callers:** Find all callers of `GenerateTOTPSecret`, `SetTOTPSecret`, `ValidateTOTPCode` — update to encrypt/decrypt with `cfg.EncryptionKey`. The auth service already has access to config.
  5. **Migration of existing rows:** Add a one-time startup migration function in `cmd/argus/main.go` that: queries all users with non-null `totp_secret` and `totp_enabled=true`, attempts to decrypt each (if decryption succeeds, already encrypted — skip; if fails, treat as plaintext), encrypts plaintext secrets and updates rows. This is application-level because SQL can't do AES-256-GCM.
  6. Add integration test: Create user + enable 2FA → inspect `users.totp_secret` → ciphertext, not plaintext. Verify TOTP still works on login.
- **Verify:** `go test ./internal/auth/ -run "TestTOTPEncrypt" -v` + `go build ./cmd/argus/`

### TOTP Encryption Details

Current state:
- `totp.go:10` `GenerateTOTPSecret()` returns plaintext secret string
- `totp.go:24` `ValidateTOTPCode(secret, code)` takes plaintext secret
- `store/user.go:113` `SetTOTPSecret(id, secret)` stores plaintext directly
- `users.totp_secret` column is `VARCHAR(255)` — wide enough for base64 ciphertext
- `ENCRYPTION_KEY` env var already exists and used for operator secrets in `internal/crypto/aes.go`

`internal/crypto/aes.go` already provides:
```go
func Encrypt(plaintext, key []byte) ([]byte, error)   // AES-256-GCM
func Decrypt(encoded, key []byte) ([]byte, error)      // AES-256-GCM
func EncryptJSON(data json.RawMessage, hexKey string) (json.RawMessage, error) // empty key = passthrough
func DecryptJSON(data json.RawMessage, hexKey string) (json.RawMessage, error) // empty key = passthrough
```

The TOTP encryption should follow the same empty-key passthrough pattern for dev mode compatibility.

### Task 7: NATS tenant isolation for WebSocket events (AC-9)

- **Files:** Modify `internal/ws/hub.go`, Modify ~35 publisher files (see NATS Publisher Inventory below)
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** Read `internal/ws/hub.go` — `relayNATSEvent` at line 191, `BroadcastAll` at line 106, `BroadcastToTenant` at line 138
- **Context refs:** Architecture Context > Data Flow, NATS Publisher Inventory, Acceptance Criteria AC-9
- **What:**
  1. **Hub modification:** In `relayNATSEvent` (hub.go:191), change from calling `BroadcastAll` to:
     - Unmarshal payload as `map[string]interface{}`
     - Extract `tenant_id` field from payload
     - If `tenant_id` is present and non-nil → call `BroadcastToTenant(tenantID, eventType, payload)`
     - If `tenant_id` is nil/missing → call `BroadcastAll(eventType, payload)` (system-wide event)
  2. **Publisher audit:** Every `eventBus.Publish()` call must include `"tenant_id"` in its payload map. Review ALL publishers from the inventory and add `tenant_id` where missing. System events (operator health) may legitimately have no tenant_id — tag as `tenant_id: nil` explicitly.
  3. Add integration tests:
     - Tenant A WS connection, tenant B publishes policy event → A does NOT receive event
     - System event (no tenant_id) → all connected tenants receive it
- **Verify:** `go test ./internal/ws/ -run "TestTenantIsolation" -v`

### NATS Publisher Inventory

Files that call `eventBus.Publish()` and need `tenant_id` review:

| File | Subject | Has tenant_id? | Action |
|------|---------|---------------|--------|
| `internal/aaa/radius/server.go` | session.started/ended | Check | Add if missing |
| `internal/aaa/diameter/gx.go` | session.started/updated/ended | Check | Add if missing |
| `internal/aaa/diameter/gy.go` | session.started/updated/ended | Check | Add if missing |
| `internal/aaa/diameter/server.go` | operator.health | No tenant — system event | Tag nil explicitly |
| `internal/aaa/sba/ausf.go` | session.started/ended | Check | Add if missing |
| `internal/aaa/sba/udm.go` | session events | Check | Add if missing |
| `internal/aaa/session/sweep.go` | session.ended | Check | Add if missing |
| `internal/api/session/handler.go` | session.ended, job.queue | Check | Add if missing |
| `internal/api/cdr/handler.go` | job.queue | Check | Add if missing |
| `internal/api/ota/handler.go` | job.queue | Check | Add if missing |
| `internal/api/job/handler.go` | job events | Check | Add if missing |
| `internal/api/sim/bulk_handler.go` | job.queue | Check | Add if missing |
| `internal/policy/enforcer/enforcer.go` | alert.triggered, notification | Check | Add if missing |
| `internal/policy/rollout/service.go` | policy.rollout_progress, job.queue | Check | Add if missing |
| `internal/operator/health.go` | operator.health, alert.triggered | System event | Tag nil explicitly |
| `internal/job/runner.go` | job.progress, job.completed | Check | Add if missing |
| `internal/job/import.go` | job events | Check | Add if missing |
| `internal/job/bulk_state_change.go` | job.progress | Check | Add if missing |
| `internal/job/bulk_policy_assign.go` | job.progress | Check | Add if missing |
| `internal/job/bulk_esim_switch.go` | job.progress | Check | Add if missing |
| `internal/job/bulk_disconnect.go` | job.progress | Check | Add if missing |
| `internal/job/purge_sweep.go` | job events | Check | Add if missing |
| `internal/job/ota.go` | job.progress | Check | Add if missing |
| `internal/job/dryrun.go` | job events | Check | Add if missing |
| `internal/job/anomaly_batch.go` | job events | Check | Add if missing |
| `internal/job/timeout.go` | job events | Check | Add if missing |
| `internal/job/scheduler.go` | job.queue | Check | Add if missing |
| `internal/job/stubs.go` | job events | Check | Add if missing |
| `internal/job/storage_monitor.go` | job events | Check | Add if missing |
| `internal/job/data_retention.go` | job events | Check | Add if missing |
| `internal/job/s3_archival.go` | job events | Check | Add if missing |
| `internal/notification/service.go` | notification.new | Has tenant_id (line 248) | Verify |
| `internal/analytics/anomaly/engine.go` | alert.triggered, anomaly.detected | Check | Add if missing |
| `internal/analytics/anomaly/batch.go` | anomaly events | Check | Add if missing |
| `internal/audit/service.go` | audit events | Check | Add if missing |

**IMPORTANT:** Developer must grep `eventBus.Publish` across entire `internal/` to catch any publishers not listed here. Expected total: ~35 files with Publish calls.

### Task 8: Compliance report PDF export (AC-8)

- **Files:** Modify `internal/compliance/service.go`, Modify `internal/api/compliance/handler.go`, Modify `web/src/pages/reports/index.tsx`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read `internal/compliance/service.go` — `ExportBTKReportCSV` at line 214; Read `internal/api/compliance/handler.go` — `BTKReport` handler at line 117
- **Context refs:** Architecture Context > API Specifications, PDF Export Details, Design Token Map, Acceptance Criteria AC-8
- **What:**
  1. **Go dependency:** Add `github.com/go-pdf/fpdf` to `go.mod`. This is a maintained fork of gofpdf.
  2. **Service:** Add `ExportBTKReportPDF(ctx, tenantID) ([]byte, error)` to `compliance/service.go`. Generate PDF with:
     - Header: "BTK Monthly SIM Report", tenant ID, report month, generated timestamp
     - Table: Operator, Code, Active, Suspended, Terminated, Total (same data as CSV)
     - Footer: Total Active, Total SIMs
  3. **Handler:** In `BTKReport` handler (handler.go:117), add `format=pdf` case alongside existing `format=csv`. Set `Content-Type: application/pdf` and `Content-Disposition: attachment; filename=btk_report_YYYYMM.pdf`.
  4. **Frontend:** Add format toggle/dropdown on Compliance Reports page. Three options: JSON (default view), CSV download, PDF download. Use existing `<Button>` and `<Select>` atoms.
  5. Add integration test: `GET /compliance/reports/:id/export?format=pdf` returns PDF bytes with correct headers.
- **Tokens:** Use ONLY classes from Design Token Map
- **Components:** Reuse Button, Select atoms
- **Note:** Invoke `frontend-design` skill for UI changes
- **Verify:** `go test ./internal/compliance/ -run "TestPDFExport" -v` + `go build ./cmd/argus/`

### PDF Export Details

Existing CSV export pattern (from `compliance/service.go:214`):
- Calls `GenerateBTKReport()` to get structured data
- Writes CSV using `encoding/csv`
- Returns `[]byte`

PDF export follows same pattern:
- Call `GenerateBTKReport()` for structured data
- Use `fpdf.New("L", "mm", "A4", "")` for landscape A4
- Add header cells, data rows
- Return `pdf.Output()` bytes

Existing handler pattern (from `handler.go:124`):
```go
format := r.URL.Query().Get("format")
if format == "csv" { ... }
// Add: if format == "pdf" { ... }
```

### Task 9: Security audit hardening (AC-10)

- **Files:** Modify `internal/gateway/router.go` (cookie flags), Modify `go.mod` (govulncheck), Modify `web/package.json` (npm audit)
- **Depends on:** Task 6 (bcrypt cost check depends on auth config)
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router.go` — cookie setting for refresh token; Read `internal/config/config.go` — `BcryptCost` field at line 39
- **Context refs:** Architecture Context > Components Involved, Security Audit Details, Acceptance Criteria AC-10
- **What:**
  1. **Cookie flags:** Find where refresh token cookie is set (likely `internal/api/auth/handler.go` or gateway). Ensure flags: `Secure: true` (in production), `HttpOnly: true`, `SameSite: http.SameSiteStrictMode`. If `Secure` is conditional on `APP_ENV != development`, that's acceptable.
  2. **Bcrypt cost:** Verify `BCRYPT_COST` default is `12` (already in config.go:39). Add validation: reject values below 12 in production mode.
  3. **Auth rate limiting:** Verify rate limits are enforced on all auth endpoints (`/api/v1/auth/login`, `/api/v1/auth/refresh`, `/api/v1/auth/2fa/verify`). The gateway brute force middleware should cover these.
  4. **govulncheck:** Run `govulncheck ./...` — fix any high/critical findings. Add to Makefile as `make vuln-check`.
  5. **npm audit:** Run `npm audit --audit-level=high` in `web/` — fix any issues. Add to Makefile as `make web-audit`.
  6. Verify all checks pass.
- **Verify:** `govulncheck ./...` exits 0, `cd web && npm audit --audit-level=high` exits 0

### Security Audit Details

Cookie setup location: Search for `SetCookie` or `http.Cookie` in auth handler.

Current config values (from `config.go`):
- `BcryptCost: 12` (default) ← meets requirement
- `RateLimitAuthPerMin: 10` (default) ← brute force protection
- `BruteForceMaxAttempts: 10` + `BruteForceWindowSeconds: 900` ← 15min window

Cookie flags required:
- `Secure: !cfg.IsDev()` — only true in production
- `HttpOnly: true` — always
- `SameSite: http.SameSiteStrictMode` — prevents CSRF

## Acceptance Criteria Mapping

| Criterion | Implemented In | Verified By |
|-----------|---------------|-------------|
| AC-1: IPv6 extractIP | Task 1 | Task 1 unit tests |
| AC-2: Session tenant filter | Task 1 | Task 1 unit tests |
| AC-3: Pseudonymization unified | Task 2 | Task 2 unit tests |
| AC-4: Webhook validation | Task 3 | Task 3 unit tests + visual check |
| AC-5: Rate limiter wiring | Task 4 | Task 4 integration test |
| AC-6: stolen_lost → terminated | Task 5 | Task 5 integration test |
| AC-7: TOTP encryption | Task 6 | Task 6 integration test |
| AC-8: PDF export | Task 8 | Task 8 integration test |
| AC-9: NATS tenant isolation | Task 7 | Task 7 integration tests |
| AC-10: Security audit | Task 9 | govulncheck + npm audit |

## Story-Specific Compliance Rules

- **API:** Standard envelope `{ status, data, meta?, error? }` for all endpoints (AC-8 PDF is binary response, not envelope)
- **DB:** No SQL migration needed for AC-7 (app-level re-encryption). State transition change is code-only.
- **UI:** Design tokens from FRONTEND.md — `--danger` for stolen_lost, `--accent` for buttons. No hardcoded hex.
- **Business:** BR-1 SIM state machine (AC-6), BR-7 audit/compliance pseudonymization (AC-3)
- **ADR:** ADR-001 modular monolith — changes stay within package boundaries. ADR-002 Redis for rate limiting (AC-5).
- **Security:** `ENCRYPTION_KEY` used consistently (AC-7). HTTPS-only webhooks (AC-4). Cookie flags Secure+HttpOnly+SameSite (AC-10).

## Bug Pattern Warnings

No matching bug patterns in `decisions.md`.

## Tech Debt

No tech debt items target STORY-059 (D-001, D-002 target STORY-077; D-003 targets STORY-062).

## Mock Retirement

No mock retirement for this story — backend endpoints already exist, this story hardens them.

## Risks & Mitigations

- **Risk: TOTP re-encryption migration failure** — If `ENCRYPTION_KEY` is not set, existing plaintext secrets remain unchanged (passthrough mode). Migration function is idempotent: re-running is safe.
- **Risk: NATS publisher audit misses a file** — The publisher inventory lists 14 files. Developer must grep for `eventBus.Publish` to catch any new publishers added since this plan.
- **Risk: go-pdf/fpdf dependency adds vulnerability** — Run `govulncheck` after adding dependency (Task 9 covers this).
- **Risk: Breaking existing TOTP for users** — The startup migration must handle both encrypted and plaintext secrets. Detect by attempting decrypt first.
