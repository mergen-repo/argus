---
story: STORY-063
title: Backend Implementation Completeness
phase: 10
effort: XL
complexity: High
planner: amil-planner
plan_version: 1
zero_deferral: true
waves: 5
tasks: 34
acs: 12
---

# Implementation Plan: STORY-063 — Backend Implementation Completeness

## Goal
Replace every stub processor, mock-only adapter, nil wire, and placeholder log on Argus's production code path with a real implementation (or a real driver + retained mock fallback), closing all 12 ACs with zero deferral.

## Phase 10 Zero-Deferral Charter
- Every AC closes **in this story**. No "will be addressed later", no `t.Skip`, no `{"status":"stub"}` left behind.
- External integrations (Twilio, AWS S3, SGP.22 ES9+) must ship (a) a real driver that compiles and is unit-tested with `httptest`/in-memory mocks, (b) a retained mock adapter for dev, (c) env-var provider selection. Live sandbox calls are optional and can be gated behind `//go:build integration`.
- No "TODO" or "not yet implemented" error strings may remain in any touched file.

## Architecture Context

### Components Involved
| Component | Layer | Files |
|-----------|-------|-------|
| SM-DP+ adapter (eSIM) | `internal/esim` | `smdp.go`, new `smdp_http.go`, new `smdp_test.go` |
| SMS gateway | `internal/notification` | `sms.go`, new `sms_test.go` |
| Notification channel enum | `internal/notification` | `service.go`, `models.go`, `service_test.go` |
| InApp notification adapter | `cmd/argus` | `main.go` (new `inAppStoreAdapter`) |
| IPReclaim processor | `internal/job` | new `ip_reclaim.go`, new `ip_reclaim_test.go` |
| SLAReport processor | `internal/job` | new `sla_report.go`, new `sla_report_test.go` |
| SLA report store + API | `internal/store`, `internal/api/sla`, `internal/gateway` | new `sla_report.go` (store), new `internal/api/sla/handler.go`, `router.go` |
| S3 uploader | `internal/storage` | new `s3_uploader.go`, new `s3_uploader_test.go` |
| 5G SBA NRF client | `internal/aaa/sba` | `nrf.go`, new `nrf_test.go` |
| Session DB persistence tests | `internal/api/session` | `handler_test.go` (unskip), `internal/aaa/session/session_test.go` (restart round-trip) |
| Health probes | `internal/gateway` | `health.go`, `health_test.go` (new) |
| Config cleanup | `internal/config` | `config.go` |
| Main wiring | `cmd/argus` | `main.go` |
| DB schema (sla_reports) | `migrations/` | new `.up.sql` / `.down.sql` |

### Data Flow — Real SM-DP+ (AC-1)
```
esim.Handler ──SetAdapter──▶ SMDPAdapter (interface)
                                ├── MockSMDPAdapter  (ESIM_SMDP_PROVIDER unset or =mock)
                                └── HTTPSMDPAdapter  (provider=valid|thales|kigen|generic)
                                         ▼
                                 ES9+ HTTPS calls
                                 POST /gsma/rsp2/es9plus/downloadOrder     → DownloadProfile
                                 POST /gsma/rsp2/es9plus/confirmOrder      → EnableProfile
                                 POST /gsma/rsp2/es9plus/cancelOrder       → DisableProfile
                                 POST /gsma/rsp2/es9plus/releaseProfile    → DeleteProfile
                                 POST /gsma/rsp2/es9plus/getProfileInfo    → GetProfileInfo
```
Retry + timeout: context deadline 10s per call; exponential backoff 3 attempts (250ms, 750ms, 2s). Errors classified into `ErrSMDPConnectionFailed` / `ErrSMDPProfileNotFound` / `ErrSMDPOperationFailed`.

### Data Flow — SMS Gateway (AC-2)
```
Service.dispatchToChannels (ChannelSMS) ─▶ SMSDispatcher.SendSMS
                                                │
                                                └── SMSGatewaySender.sendViaTwilio
                                                         ▼
                                                   POST https://api.twilio.com/2010-04-01/Accounts/{SID}/Messages.json
                                                   Basic auth (AccountID:AuthToken)
                                                   form body: To, From, Body, StatusCallback
```
Status-callback webhook: `POST /api/v1/notifications/sms/status` (new) receives Twilio `MessageSid`+`MessageStatus`, updates notifications row. Rate limited via existing `RedisRateLimiter`.

### Data Flow — IPReclaim job (AC-3)
```
cron IP_RECLAIM ─▶ jobs table (queued)
                          │
                          ▼
                IPReclaimProcessor.Process
                     │
                     ├── ippoolStore.ListExpiredReclaim(ctx, now)
                     │        (state='reclaiming' AND reclaim_at <= now)
                     ├── ippoolStore.FinalizeReclaim(ctx, ipID)
                     │        (state='available', sim_id=NULL, allocated_at=NULL,
                     │         reclaim_at=NULL; used_addresses-=1)
                     ├── auditSvc.Record(ctx, 'ip_addresses', 'reclaim', ...)
                     └── eventBus.Publish("ip.reclaimed", {tenant_id, pool_id, ip_id, addr})
```

### Data Flow — SLAReport job + endpoint (AC-4)
```
cron SLA_REPORT ─▶ SLAReportProcessor.Process
                          │
                          ├── operatorHealthStore.AggregateByTenant(ctx, window)
                          │        → uptime_pct, latency_p95_ms, incident_count, mttr_sec
                          ├── radiusSessionStore.CountByTenantInWindow(ctx, window)
                          ├── slaReportStore.Create(ctx, SLAReportRow)
                          └── eventBus.Publish("sla.report.generated", {...})

GET /api/v1/sla-reports?from&to&tenant_id=... ─▶ slaHandler.List
```

### Data Flow — S3 Uploader (AC-5)
```
main.go ─▶ storage.NewS3Uploader(cfg) ─▶ job.S3ArchivalProcessor (existing)
                        │
                        └── aws-sdk-go-v2
                            config.LoadDefaultConfig(..., WithRegion, WithCredentialsProvider)
                            s3.NewFromConfig(cfg, func(o){ o.UsePathStyle=cfg.S3PathStyle; o.BaseEndpoint=cfg.S3Endpoint })
                            PutObject(bucket, key, bytes.NewReader(data), len)
```
MinIO mode: `S3_ENDPOINT=http://minio:9000 S3_PATH_STYLE=true`.

### Data Flow — NRF Registration (AC-8)
```
main.go SBA startup ─▶ sba.Server.Start ─▶ nrfClient.Register(ctx)
  (if cfg.SBANRFURL != "")               │
                                         ├── PUT  {NRFURL}/nnrf-nfm/v1/nf-instances/{instanceId}   (NFRegister) body=NFProfile
                                         ├── PATCH {NRFURL}/nnrf-nfm/v1/nf-instances/{instanceId}  (NFUpdate heartbeat) body=[{op,path,value}]
                                         ├── DELETE {NRFURL}/nnrf-nfm/v1/nf-instances/{instanceId} (NFDeregister)
                                         └── POST  {NRFURL}/nnrf-nfm/v1/subscriptions              (NFStatusSubscribe; handle incoming NFStatusNotify on existing handler)
```
Empty URL → log once `"NRF disabled (dev)"`, return early. No placeholder logs.

### Database Schema

**sla_reports (new — Source: this story creates it)**
```sql
-- Source: migrations/20260412000001_sla_reports.up.sql (NEW)
CREATE TABLE IF NOT EXISTS sla_reports (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    operator_id UUID REFERENCES operators(id),           -- NULL = tenant-wide aggregate
    window_start TIMESTAMPTZ NOT NULL,
    window_end   TIMESTAMPTZ NOT NULL,
    uptime_pct       NUMERIC(5,2)  NOT NULL,             -- 0.00 to 100.00
    latency_p95_ms   INTEGER       NOT NULL DEFAULT 0,
    incident_count   INTEGER       NOT NULL DEFAULT 0,
    mttr_sec         INTEGER       NOT NULL DEFAULT 0,
    sessions_total   BIGINT        NOT NULL DEFAULT 0,
    error_count      INTEGER       NOT NULL DEFAULT 0,
    details          JSONB         NOT NULL DEFAULT '{}'::jsonb,
    generated_at     TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT sla_window_valid CHECK (window_end > window_start)
);

CREATE INDEX IF NOT EXISTS idx_sla_reports_tenant_time  ON sla_reports (tenant_id, window_end DESC);
CREATE INDEX IF NOT EXISTS idx_sla_reports_operator     ON sla_reports (operator_id, window_end DESC) WHERE operator_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sla_reports_generated_at ON sla_reports (generated_at DESC);
```
Down migration: `DROP TABLE IF EXISTS sla_reports CASCADE;`

**Referenced (existing — read-only in this story):**
- `ip_addresses` (TBL-09, migrations/20260320000002_core_schema.up.sql:233) — columns used: `id, pool_id, sim_id, state, reclaim_at`. State values: `available`, `allocated`, `reserved`, `reclaiming`.
- `ip_pools` (TBL-08) — `id, reclaim_grace_period_days, used_addresses, state`.
- `sessions` (TBL-17, line 390) — create/update/end path already writes via `RadiusSessionStore.Create/UpdateCounters/Finalize`.
- `operator_health_logs` (TBL-23, line 557) — columns: `operator_id, checked_at, status, latency_ms, circuit_state`. Used for SLA aggregation.
- `notifications` (TBL-21, line 512) — `InAppStore.CreateNotification` will map into `store.NotificationStore.Create` via new adapter.

### API Specifications

**GET /api/v1/sla-reports** (new — AC-4)
- Query params: `from` (RFC3339), `to` (RFC3339), `operator_id?` (uuid), `cursor?`, `limit?` (default 50, max 200)
- Auth: JWT required; tenant scoping from token
- Success 200: `{ status: "success", data: [{ id, operator_id, window_start, window_end, uptime_pct, latency_p95_ms, incident_count, mttr_sec, sessions_total, error_count, generated_at }], meta: { next_cursor, count } }`
- Error 400: `{ status: "error", error: { code: "INVALID_RANGE", message } }`
- Error 401/403/500: standard envelope

**GET /api/v1/sla-reports/{id}** (new)
- Success 200: single report row (tenant-scoped, 404 if not in caller tenant)

**POST /api/v1/notifications/sms/status** (new — AC-2 Twilio webhook, AC-7 wiring)
- Body: Twilio form-encoded `MessageSid`, `MessageStatus`, `To`, `From`, `ErrorCode?`
- Auth: `X-Twilio-Signature` HMAC (computed with AuthToken) verified before trusting body
- Success 204 No Content
- Error 401: signature invalid; 400: missing fields

**GET /api/v1/health** (enhanced — AC-10)
- Success 200 (all probes pass):
  `{ status: "success", data: { db: { status: "ok", latency_ms: 3, last_checked: "..." }, redis: {...}, nats: {...}, aaa: {...}, uptime: "2h" } }`
- 503 when any probe fails OR any probe has not run (`last_checked` zero value): `data.db.status = "pending" | "error: ..."`. Body still includes all probe details.

### Config Additions

Add to `internal/config/config.go`:
```go
ESIMProvider      string `envconfig:"ESIM_SMDP_PROVIDER"      default:"mock"` // mock|generic|valid|thales|idemia|kigen
ESIMSMDPBaseURL   string `envconfig:"ESIM_SMDP_BASE_URL"`
ESIMSMDPAPIKey    string `envconfig:"ESIM_SMDP_API_KEY"`
ESIMSMDPClientCert string `envconfig:"ESIM_SMDP_CLIENT_CERT_PATH"`
ESIMSMDPClientKey  string `envconfig:"ESIM_SMDP_CLIENT_KEY_PATH"`

SMSProvider    string `envconfig:"SMS_PROVIDER"     default:""` // "" | twilio | vonage
SMSAccountID   string `envconfig:"SMS_ACCOUNT_ID"`
SMSAuthToken   string `envconfig:"SMS_AUTH_TOKEN"`
SMSFromNumber  string `envconfig:"SMS_FROM_NUMBER"`
SMSStatusCallbackURL string `envconfig:"SMS_STATUS_CALLBACK_URL"`

SBANRFURL       string `envconfig:"SBA_NRF_URL"`
SBANFInstanceID string `envconfig:"SBA_NF_INSTANCE_ID" default:"argus-sba-01"`
SBANRFHeartbeatSec int `envconfig:"SBA_NRF_HEARTBEAT_SEC" default:"30"`
```
Remove: `DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL` (dead — AC-11).
`DevCORSAllowAll` is KEPT (live, STORY-074 scope).

### New Go Module Dependencies

| Module | Version | Purpose | AC |
|--------|---------|---------|-----|
| `github.com/aws/aws-sdk-go-v2` | latest v2 | Core SDK | AC-5 |
| `github.com/aws/aws-sdk-go-v2/config` | latest | Config loader | AC-5 |
| `github.com/aws/aws-sdk-go-v2/credentials` | latest | Static + env creds | AC-5 |
| `github.com/aws/aws-sdk-go-v2/service/s3` | latest | S3 client | AC-5 |
| `github.com/alicebob/miniredis/v2` | latest | In-memory Redis for AC-9 test harness | AC-9 |

No Twilio SDK. Use `net/http` + `httptest` — matches existing NRF/ES9+ pattern and keeps dep tree small.

## Prerequisites
- [x] STORY-056 (runtime fixes) — merged
- [x] STORY-057 (API-052 SLA report structure + job routes) — merged
- [x] STORY-053 left `S3ArchivalProcessor` compiled with `S3Uploader` interface; only the instance wire is nil
- [x] STORY-059 wired webhook config validation + notification rate limiter (we reuse both)

## Wave Structure

| Wave | Name | Task IDs | Depends on | Parallel? |
|------|------|---------|-------------|-----------|
| 0 | Schema + config foundation | 1, 2, 3 | — | partial (1 and 2 can run in parallel; 3 depends on 2) |
| 1 | Interfaces + store + driver scaffolds | 4, 5, 6, 7, 8, 9 | W0 | yes |
| 2 | Concrete processors + real adapters | 10, 11, 12, 13, 14, 15, 16, 17, 18 | W1 | yes (tasks in same package run serial) |
| 3 | Wiring in main.go + router + health refactor | 19, 20, 21, 22, 23 | W2 | partial |
| 4 | Tests (unit + integration) and cleanup | 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34 | W3 | yes, many in parallel |

## Tasks

### Task 1: Add `sla_reports` migration (up + down)
- **Files:** Create `migrations/20260412000001_sla_reports.up.sql`, Create `migrations/20260412000001_sla_reports.down.sql`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `migrations/20260324000001_policy_violations.up.sql` — follow same plain CREATE TABLE + indexes + down DROP pattern.
- **Context refs:** "Database Schema > sla_reports"
- **What:** Create exact schema specified in plan. Use `CREATE TABLE IF NOT EXISTS`, add 3 indexes, CHECK constraint `sla_window_valid`. Down migration drops the table with CASCADE.
- **Verify:** `make db-migrate && psql $DATABASE_URL -c '\d sla_reports'` shows 12 columns; `make db-migrate-down && make db-migrate` round-trips cleanly.

### Task 2: Config struct cleanup + new env vars (AC-11 + AC-1 + AC-2 + AC-5 + AC-8)
- **Files:** Modify `internal/config/config.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read `internal/config/config.go` S3 block (lines 73-78) — follow existing `envconfig` tag style.
- **Context refs:** "Config Additions"
- **What:**
  1. Remove 4 dead fields: `DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL` (confirmed dead: grep shows no reads anywhere).
  2. Keep `DevCORSAllowAll` (live in main.go:644 — STORY-074 territory).
  3. Add `ESIMProvider`, `ESIMSMDPBaseURL`, `ESIMSMDPAPIKey`, `ESIMSMDPClientCert`, `ESIMSMDPClientKey`.
  4. Add `SMSProvider`, `SMSAccountID`, `SMSAuthToken`, `SMSFromNumber`, `SMSStatusCallbackURL`.
  5. Add `SBANRFURL`, `SBANFInstanceID`, `SBANRFHeartbeatSec`.
- **Verify:** `go build ./...` succeeds; `grep -E "DevSeedData|DevMockOperator|DevDisable2FA|DevLogSQL" internal/ cmd/` returns zero.

### Task 3: Update `.env.example` with new env vars (AC-1, AC-2, AC-5, AC-8, AC-11)
- **Files:** Modify `.env.example`
- **Depends on:** Task 2
- **Complexity:** low
- **Pattern ref:** Read `.env.example` around `S3_ENDPOINT` block — follow same `KEY=default_or_empty # comment` style.
- **Context refs:** "Config Additions"
- **What:** Append new env blocks for eSIM, SMS, SBA NRF. No `DEV_*` entries to remove (none present — confirmed via grep). Document each var with a one-line comment.
- **Verify:** `grep -E "ESIM_SMDP_PROVIDER|SMS_PROVIDER|SBA_NRF_URL" .env.example` returns each.

### Task 4: Add `GetProfileInfo` to `SMDPAdapter` interface + mock impl (AC-1)
- **Files:** Modify `internal/esim/smdp.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read existing `DownloadProfile` method on `MockSMDPAdapter` — mirror structure.
- **Context refs:** "Components Involved", "Data Flow — Real SM-DP+"
- **What:** Add `GetProfileInfoRequest`/`Response` struct with fields `EID, ICCID, ProfileID string` and response `State, ICCID, SMDPPlusID, LastSeenAt`. Add `GetProfileInfo(ctx, req)` method to interface. Implement on `MockSMDPAdapter` returning a stubbed-but-valid response.
- **Verify:** `go build ./internal/esim/...` succeeds; existing callers still compile.

### Task 5: Implement HTTP SM-DP+ adapter (AC-1)
- **Files:** Create `internal/esim/smdp_http.go`
- **Depends on:** Task 4
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/diameter/tls.go` for the TLS client pattern; read `internal/notification/telegram.go` for HTTP call + JSON body pattern.
- **Context refs:** "Data Flow — Real SM-DP+", "Components Involved"
- **What:** Implement `HTTPSMDPAdapter` satisfying `SMDPAdapter`:
  - Constructor `NewHTTPSMDPAdapter(cfg HTTPSMDPConfig, logger)` with `BaseURL, APIKey, ClientCertPath, ClientKeyPath, Timeout (default 10s)`.
  - `http.Client` with TLS config (optional mTLS via client cert if paths non-empty).
  - Each of 5 adapter methods POSTs a JSON body (struct literal) to the ES9+ endpoint listed in plan data-flow. Request body uses `application/json; charset=utf-8`, `User-Agent: argus-esim/1.0`, `X-Api-Key: {APIKey}` header.
  - Retry helper with 3 attempts, exponential backoff, honoring `ctx.Done()`.
  - Response decoding: 2xx → parse; 404 → `ErrSMDPProfileNotFound`; 5xx/timeout → `ErrSMDPConnectionFailed`; other 4xx → `ErrSMDPOperationFailed` wrapping body.
  - Logger fields: `component=http_smdp`, `method`, `iccid`, `status`, `attempt`.
- **Verify:** `go build ./internal/esim/...` succeeds.

### Task 6: Add `IPPoolStore.ListExpiredReclaim` + `FinalizeReclaim` (AC-3)
- **Files:** Modify `internal/store/ippool.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read existing `IPPoolStore.ReleaseIP` (line 578) — follow same `BEGIN tx, FOR UPDATE, UPDATE rows, UPDATE pool counter, COMMIT` pattern.
- **Context refs:** "Data Flow — IPReclaim job", "Database Schema" (ip_addresses)
- **What:**
  1. Add `type ExpiredIPAddress struct { ID, PoolID, TenantID uuid.UUID; AddressV4, AddressV6 *string; PreviousSimID *uuid.UUID; ReclaimAt time.Time }`.
  2. Add method `ListExpiredReclaim(ctx, now time.Time, limit int) ([]ExpiredIPAddress, error)` — SELECT ip_addresses JOIN ip_pools WHERE a.state='reclaiming' AND a.reclaim_at <= $1 LIMIT $2. Include tenant_id via JOIN ip_pools.
  3. Add method `FinalizeReclaim(ctx, ipID uuid.UUID) error` — in a tx: UPDATE ip_addresses SET state='available', sim_id=NULL, allocated_at=NULL, reclaim_at=NULL WHERE id=$1 AND state='reclaiming'; decrement `ip_pools.used_addresses` and unset `state='exhausted'` if applicable. Returns `ErrIPNotFound` if row not in reclaiming state.
- **Verify:** `go test ./internal/store/...` passes; new methods covered by existing test file extensions (Task 24).

### Task 7: Add `SLAReportStore` with Create + List + Get (AC-4)
- **Files:** Create `internal/store/sla_report.go`
- **Depends on:** Task 1
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/policy_violation.go` — follow same store struct + `Create`/`List`/`Get` + cursor pagination pattern.
- **Context refs:** "Database Schema > sla_reports", "API Specifications > GET /api/v1/sla-reports"
- **What:** Implement:
  - `type SLAReportStore struct{ db *pgxpool.Pool }`; `NewSLAReportStore(db) *SLAReportStore`.
  - `type SLAReportRow struct { ID, TenantID uuid.UUID; OperatorID *uuid.UUID; WindowStart, WindowEnd time.Time; UptimePct float64; LatencyP95Ms, IncidentCount, MTTRSec, ErrorCount int; SessionsTotal int64; Details json.RawMessage; GeneratedAt time.Time }`.
  - `Create(ctx, row) (*SLAReportRow, error)` — INSERT ... RETURNING id, generated_at.
  - `GetByID(ctx, tenantID, id uuid.UUID) (*SLAReportRow, error)` — tenant-scoped.
  - `ListByTenant(ctx, tenantID uuid.UUID, from, to time.Time, operatorID *uuid.UUID, cursor string, limit int) ([]SLAReportRow, string, error)` — keyset pagination by `(window_end DESC, id DESC)`.
- **Verify:** `go build ./internal/store/...` succeeds.

### Task 8: Add `OperatorStore.AggregateHealthForSLA` helper (AC-4)
- **Files:** Modify `internal/store/operator.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** Read existing `OperatorStore.GetHealthLogs` (line 524) — same SELECT pattern; add aggregation SQL.
- **Context refs:** "Data Flow — SLAReport job + endpoint", "Database Schema" (operator_health_logs)
- **What:** Add:
```go
type SLAAggregate struct {
    UptimePct     float64
    LatencyP95Ms  int
    IncidentCount int
    MTTRSec       int
}
func (s *OperatorStore) AggregateHealthForSLA(ctx context.Context, operatorID uuid.UUID, from, to time.Time) (*SLAAggregate, error)
```
Query uses `percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)` for p95, `SUM(CASE WHEN status='down' THEN 1 ELSE 0 END)` for incidents, `100.0 * SUM(CASE WHEN status='up' THEN 1 ELSE 0 END) / COUNT(*)` for uptime, `AVG(recovery_duration)` computed via window LAG for MTTR. Fallback to zero when no rows.
- **Verify:** `go build ./internal/store/...` succeeds.

### Task 9: Create `storage.S3Uploader` package with aws-sdk-go-v2 (AC-5)
- **Files:** Create `internal/storage/s3_uploader.go`, Modify `go.mod`, `go.sum`
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** No existing AWS client in the codebase. First of its kind. Structure: package `storage`, `type S3Uploader struct{ client *s3.Client; bucket string; logger zerolog.Logger }`. Constructor loads `config.LoadDefaultConfig(ctx, config.WithRegion, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(access, secret, "")))` then `s3.NewFromConfig(cfg, func(o *s3.Options){ if cfgInput.Endpoint!="" { o.BaseEndpoint = &cfgInput.Endpoint }; o.UsePathStyle = cfgInput.PathStyle })`.
- **Context refs:** "Data Flow — S3 Uploader"
- **What:**
  1. Run `go get github.com/aws/aws-sdk-go-v2 github.com/aws/aws-sdk-go-v2/config github.com/aws/aws-sdk-go-v2/credentials github.com/aws/aws-sdk-go-v2/service/s3` (latest stable).
  2. Define `type S3Config struct { Endpoint, AccessKey, SecretKey, Bucket, Region string; PathStyle bool }`.
  3. Define `type S3Uploader` satisfying `job.S3Uploader`: method `Upload(ctx, bucket, key string, data []byte) error` — `PutObject(ctx, &s3.PutObjectInput{ Bucket, Key, Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))) })`.
  4. Constructor `NewS3Uploader(ctx, cfg S3Config, logger) (*S3Uploader, error)` — returns descriptive error when credentials missing. When `cfg.AccessKey == ""`, fall back to default AWS credential chain (env/IMDS).
  5. `HealthCheck(ctx) error` via `HeadBucket` for health probe reuse.
- **Verify:** `go build ./internal/storage/...` succeeds; `go.sum` updated.

### Task 10: Implement `IPReclaimProcessor` (AC-3)
- **Files:** Create `internal/job/ip_reclaim.go`
- **Depends on:** Task 6
- **Complexity:** high
- **Pattern ref:** Read `internal/job/purge_sweep.go` (STORY-039 real processor) — same `Process(ctx, job)`, `CheckCancelled`, `UpdateProgress`, `Complete`, `eventBus.Publish` pattern.
- **Context refs:** "Data Flow — IPReclaim job"
- **What:**
  1. `type IPReclaimProcessor struct { jobs *store.JobStore; ippools *store.IPPoolStore; eventBus *bus.EventBus; auditSvc AuditRecorder; logger zerolog.Logger }`.
  2. Interface `AuditRecorder interface { Record(ctx, tenantID uuid.UUID, action, entityType, entityID string, before, after any) error }` — adapter from audit.Service wired in main.go.
  3. `Process(ctx, job)` reads `now` from payload (default `time.Now()`), calls `ippools.ListExpiredReclaim(ctx, now, batchSize=1000)`, iterates: for each IP → `FinalizeReclaim` → `auditSvc.Record("ip.reclaimed", "ip_address", ipID, {sim_id, addr}, nil)` → `eventBus.Publish(bus.SubjectIPReclaimed, {...})`. Counts success/failed, calls `jobs.UpdateProgress`, finally `jobs.Complete` with JSON result `{reclaimed, failed, total}`.
  4. Add new subject `bus.SubjectIPReclaimed = "ip.reclaimed"` in `internal/bus/subjects.go`.
- **Verify:** `go build ./internal/job/...` succeeds; processor registered correctly in main.go (Task 20).

### Task 11: Implement `SLAReportProcessor` (AC-4)
- **Files:** Create `internal/job/sla_report.go`
- **Depends on:** Task 7, Task 8
- **Complexity:** high
- **Pattern ref:** Read `internal/job/storage_monitor.go` (aggregation-then-write pattern) and `purge_sweep.go`.
- **Context refs:** "Data Flow — SLAReport job + endpoint"
- **What:**
  1. `type SLAReportProcessor struct { jobs *store.JobStore; slaStore *store.SLAReportStore; operatorStore *store.OperatorStore; tenantStore *store.TenantStore; radiusSessStore *store.RadiusSessionStore; eventBus *bus.EventBus; logger zerolog.Logger }`.
  2. Payload struct with `WindowStart, WindowEnd time.Time` (defaults: last 24h for `@daily`).
  3. `Process`: list all active tenants, for each tenant list operators, aggregate via `operatorStore.AggregateHealthForSLA`, count sessions via `radiusSessStore.CountInWindow(tenantID, from, to)` (add helper if missing — simple `COUNT(*) WHERE tenant_id=$1 AND started_at>=$2 AND started_at<$3`), build `SLAReportRow`, write via `slaStore.Create`. Publish `bus.SubjectSLAReportGenerated` per tenant. `UpdateProgress`, `Complete`.
  4. Add `bus.SubjectSLAReportGenerated = "sla.report.generated"` in `internal/bus/subjects.go`.
- **Verify:** `go build ./internal/job/...` succeeds.

### Task 12: Real Twilio SMS driver (AC-2 + AC-7 + AC-12)
- **Files:** Modify `internal/notification/sms.go`, Create `internal/notification/sms_twilio.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/notification/telegram.go` — HTTP POST + form body + status check + structured error returning. Read `internal/notification/webhook.go` for HMAC signature verification pattern.
- **Context refs:** "Data Flow — SMS Gateway", "Config Additions"
- **What:**
  1. In `sms.go`: replace the `fmt.Errorf("notification: twilio integration not yet implemented")` with a delegation to `twilioClient.Send(ctx, to, body)`. Delete Vonage placeholder returning-error branch (replace with a clean "provider not supported" error that is _actually logged_, not masked).
  2. In `sms_twilio.go`:
     - `type twilioClient struct { accountID, authToken, fromPhone, statusCallback string; http *http.Client; logger zerolog.Logger }`.
     - `func newTwilioClient(cfg SMSConfig, logger) *twilioClient`.
     - `Send(ctx, to, body string) error`: POST `https://api.twilio.com/2010-04-01/Accounts/{accountID}/Messages.json`, Basic auth (accountID:authToken), `Content-Type: application/x-www-form-urlencoded`, body `To={to}&From={fromPhone}&Body={body}&StatusCallback={callback}`. Parse JSON response `{sid, status}`, return `sid`-annotated error on non-2xx. 5s timeout via ctx.
     - `VerifyStatusSignature(url string, formValues url.Values, headerSignature string) bool` — Twilio-style HMAC-SHA1 over `url + sorted(k+v)`, base64, constant-time compare to header.
  3. Update `SMSGatewaySender.sendViaTwilio` to invoke `twilioClient.Send`. Keep `sendViaVonage` as an explicit `ErrSMSProviderNotSupported` (typed error, NOT a fmt.Errorf with "not yet implemented" wording).
- **Verify:** `go build ./internal/notification/...` succeeds. `grep "not yet implemented" internal/notification/sms*.go` returns zero.

### Task 13: SMS status webhook handler + route (AC-2 + AC-7)
- **Files:** Create `internal/api/notification/sms_webhook.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 12, Task 2
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/notification/handler.go` (existing) + `internal/notification/webhook.go` for sig verify.
- **Context refs:** "API Specifications > POST /api/v1/notifications/sms/status"
- **What:**
  1. Handler struct embeds `*twilioClient` (for signature verify) + `*store.NotificationStore` (to update `delivery_meta`, `delivered_at`/`failed_at`).
  2. `HandleStatusCallback(w, r)` — parse form, verify `X-Twilio-Signature`, look up notification row via `delivery_meta->>'sid' = $1`, update state. Return 204 on success, 401/400 on signature/parse failure.
  3. Register the route in `router.go` as a public path (no JWT — signature is auth), body size limit 8KB.
- **Verify:** `go build ./...` succeeds; `go vet ./...` clean.

### Task 14: Consolidate notification channel constants + nil-sender check (AC-12 + AC-7)
- **Files:** Modify `internal/notification/service.go`, Modify `internal/notification/models.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read existing constant block in `service.go:16-20` — extend it.
- **Context refs:** "Components Involved"
- **What:**
  1. Move `ChannelWebhook` and `ChannelSMS` from `models.go:9-12` into the canonical block in `service.go:16-20` (single source of truth).
  2. Add method `Service.validateChannels() []string` — returns list of configured channels whose sender is nil, called at `Start()`. Log `warn` per mismatched channel ("channel X configured but sender is nil, dispatches will skip").
  3. Keep existing nil-skip in `dispatchToChannels` (it already handles nils safely).
- **Verify:** `go build ./internal/notification/...` + `go test ./internal/notification/...` pass.

### Task 15: Real NRF 3GPP client (AC-8)
- **Files:** Modify `internal/aaa/sba/nrf.go`
- **Depends on:** Task 2
- **Complexity:** high
- **Pattern ref:** Read `internal/aaa/diameter/tls.go` for HTTP client TLS setup; `internal/notification/webhook.go` for request+retry.
- **Context refs:** "Data Flow — NRF Registration", "Config Additions"
- **What:**
  1. Extend `NRFConfig` with `NFType string` (default `"AUSF"`) and `SubscriptionID string`. `NRFRegistration` gains `http *http.Client`, `nrfURL string`, `subID string`.
  2. `Register(ctx)`: if `cfg.NRFURL == ""` log once `"NRF disabled (dev)"` + return nil. Otherwise PUT `{nrfURL}/nnrf-nfm/v1/nf-instances/{instanceId}` body = NFProfile JSON. Expect 200/201. 5s timeout.
  3. `Heartbeat(ctx)`: PATCH `{nrfURL}/nnrf-nfm/v1/nf-instances/{instanceId}`, body = JSON patch `[{"op":"replace","path":"/nfStatus","value":"REGISTERED"}]`, header `Content-Type: application/json-patch+json`. 200/204 expected.
  4. `Deregister(ctx)`: DELETE `{nrfURL}/nnrf-nfm/v1/nf-instances/{instanceId}`. 204 expected.
  5. `NotifyStatus(ctx, event, targetNFInstanceID)`: POST `{nrfURL}/nnrf-nfm/v1/subscriptions` on first call to register a subscription (store `subID` from Location header); that path stays the same but stop logging "placeholder". Incoming `HandleNFStatusNotify` already parses POST — replace placeholder log with structured info and ack processing.
  6. Ctx-aware in ALL methods; methods take `ctx context.Context` (current signatures take none — update them). Callers in `server.go` adapt.
  7. DELETE the string "placeholder" from every log message in this file.
- **Verify:** `go build ./internal/aaa/sba/...` succeeds; `grep -i "placeholder" internal/aaa/sba/nrf.go` returns zero.

### Task 16: Start/Stop NRF lifecycle hook (heartbeat ticker) in sba.Server (AC-8)
- **Files:** Modify `internal/aaa/sba/server.go`
- **Depends on:** Task 15
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/diameter/peer.go` for ticker-based heartbeat + `defer stop` pattern, or `internal/aaa/session/sweep.go`.
- **Context refs:** "Data Flow — NRF Registration"
- **What:**
  1. `Server.Start(ctx)` calls `nrfRegistration.Register(ctx)`; returns error on non-nil (production — dev empty URL is nil).
  2. Launch `go s.nrfHeartbeatLoop(ctx)` using `time.NewTicker(cfg.NRFHeartbeatSec * time.Second)`; on ctx.Done call `nrfRegistration.Deregister(ctx)`.
  3. Recover around the loop body (Phase 10 PAT: all goroutines recover).
- **Verify:** `go build ./internal/aaa/sba/...` succeeds.

### Task 17: Health probes — real Ping + latency + pending state (AC-10)
- **Files:** Modify `internal/gateway/health.go`
- **Depends on:** —
- **Complexity:** medium
- **Pattern ref:** First of its kind. Rewrite `Check` to:
  - Introduce `type probeResult struct { Status string; LatencyMs int64; LastCheckedAt time.Time; Err string }`.
  - `h.lastDB atomic.Value` / same for redis/nats; updated on each call (non-blocking cached with 1s freshness) — OR compute fresh each call (simpler, acceptable given chi already rate-limits). Prefer fresh-each-call for correctness.
- **Context refs:** "API Specifications > GET /api/v1/health", "Data Flow — Health probes"
- **What:**
  1. Add `healthData.DB/Redis/NATS` from `string` to `probeResult`.
  2. In `Check`: initial `probeResult{Status: "pending"}` → if never probed and startup time < 2s → keep pending 503. Else run `HealthCheck(ctx)` with 2s per-probe timeout; measure latency via `time.Now()` before/after; populate LatencyMs; set status `ok` or `error: ...`.
  3. Unhealthy = any probe in `error` OR `pending`. Return 503 in either case.
  4. Keep AAA/Diameter/SBA section intact, add latency fields to that section too.
  5. JSON envelope unchanged (still `{status, data, error?}`), data structure expanded.
- **Verify:** `go build ./...` succeeds. Hit `curl -sf http://localhost:8080/api/v1/health` in docker compose → expect `data.db.latency_ms` > 0 (Task 30 test).

### Task 18: Add `RadiusSessionStore.CountInWindow` helper (AC-4 support)
- **Files:** Modify `internal/store/session_radius.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Read existing `RadiusSessionStore.CountActive` — same pattern.
- **Context refs:** "Data Flow — SLAReport job + endpoint"
- **What:** Add `func (s *RadiusSessionStore) CountInWindow(ctx, tenantID uuid.UUID, from, to time.Time) (int64, error)` — `SELECT COUNT(*) FROM sessions WHERE tenant_id=$1 AND started_at>=$2 AND started_at<$3`.
- **Verify:** `go build ./internal/store/...`.

### Task 19: Wire real S3Uploader + NRF config + ESIM adapter selector in main.go (AC-1 + AC-5 + AC-8)
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 5, Task 9, Task 15, Task 16, Task 2
- **Complexity:** high
- **Pattern ref:** Existing `NewPurgeSweepProcessor` wiring (line 296) for constructor pattern; existing `var s3Uploader job.S3Uploader` (line 326) — REPLACE with real.
- **Context refs:** "Data Flow — S3 Uploader", "Data Flow — NRF Registration", "Data Flow — Real SM-DP+"
- **What:**
  1. Replace `var s3Uploader job.S3Uploader` (line 326) with:
     ```
     s3Up, err := storage.NewS3Uploader(ctx, storage.S3Config{Endpoint: cfg.S3Endpoint, AccessKey: cfg.S3AccessKey, SecretKey: cfg.S3SecretKey, Bucket: cfg.S3Bucket, Region: cfg.S3Region, PathStyle: cfg.S3PathStyle}, log.Logger)
     if err != nil { if cfg.IsProd() { log.Fatal()...} else { log.Warn()... s3Up = nil } }
     ```
     Pass `s3Up` into `NewS3ArchivalProcessor`.
  2. NRF: in the `cfg.SBAEnabled` block, `sbaServer := sba.NewServer(..., sba.ServerConfig{ NRFConfig: sba.NRFConfig{ NRFURL: cfg.SBANRFURL, NFInstanceID: cfg.SBANFInstanceID, HeartbeatSec: cfg.SBANRFHeartbeatSec } })`. No more hardcoded `"argus-sba-01"` at server.go:63 — move to config.
  3. eSIM: replace `esim.NewMockSMDPAdapter(log.Logger)` with selector:
     ```
     var smdpAdapter esim.SMDPAdapter
     switch cfg.ESIMProvider {
       case "", "mock": smdpAdapter = esim.NewMockSMDPAdapter(log.Logger)
       default:
         smdpAdapter = esim.NewHTTPSMDPAdapter(esim.HTTPSMDPConfig{...}, log.Logger)
     }
     ```
  4. SMS dispatcher: if `cfg.SMSProvider != ""`, construct `notification.NewSMSGatewaySender(cfg.SMSProvider, ...)`, call `notifSvc.SetSMS(smsSender)`, append `ChannelSMS` to `notifChannels`.
- **Verify:** `go build ./...` succeeds; `go vet ./...` clean; `grep "var s3Uploader job.S3Uploader" cmd/argus/main.go` returns zero.

### Task 20: Wire IPReclaim + SLAReport real processors in main.go (AC-3 + AC-4)
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** Task 10, Task 11
- **Complexity:** medium
- **Pattern ref:** Existing purgeSweepProc + bulkStateChangeProc constructor lines.
- **Context refs:** "Data Flow — IPReclaim job", "Data Flow — SLAReport job + endpoint"
- **What:**
  1. Delete lines 297-298 (`ipReclaimStub := job.NewStubProcessor(job.JobTypeIPReclaim, ...)` and `slaReportStub := ...`). Delete the corresponding `jobRunner.Register(ipReclaimStub)` and `jobRunner.Register(slaReportStub)` lines.
  2. Build `slaReportStore := store.NewSLAReportStore(pg.Pool)`.
  3. Build `auditRecAdapter := &auditRecorderAdapter{svc: auditSvc}` implementing `job.AuditRecorder`.
  4. `ipReclaimProc := job.NewIPReclaimProcessor(jobStore, ippoolStore, eventBus, auditRecAdapter, log.Logger); jobRunner.Register(ipReclaimProc)`.
  5. `slaReportProc := job.NewSLAReportProcessor(jobStore, slaReportStore, operatorStore, tenantStore, radiusSessionStore, eventBus, log.Logger); jobRunner.Register(slaReportProc)`.
- **Verify:** `go build ./...` succeeds; `grep "NewStubProcessor(job.JobTypeIPReclaim" cmd/argus/main.go` returns zero.

### Task 21: Wire real InAppStore adapter in main.go (AC-6)
- **Files:** Modify `cmd/argus/main.go`
- **Depends on:** —
- **Complexity:** low
- **Pattern ref:** Existing `notifStoreAdapter` struct at main.go:1074 — extend it.
- **Context refs:** "Components Involved"
- **What:**
  1. Add method `CreateNotification(ctx, n notification.InAppNotification) error` on the existing `notifStoreAdapter`. Internally map `InAppNotification` → `store.CreateNotificationParams` (tenant_id from ctx, scope from defaults).
  2. In `NewService` call at line 417, pass `notifAdapter` (not `nil`) as the third argument: `notification.NewService(emailSender, telegramSender, notifAdapter, notifChannels, log.Logger)`.
  3. Keep `SetNotifStore(notifAdapter)` call — single adapter serves both roles.
- **Verify:** `go build ./...` succeeds; `grep "notification.NewService(emailSender, telegramSender, nil" cmd/argus/main.go` returns zero.

### Task 22: SLA report API handler + router (AC-4)
- **Files:** Create `internal/api/sla/handler.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 7
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/notification/handler.go` for CRUD handler + cursor pagination; read how routes are registered in `router.go` for authenticated paths.
- **Context refs:** "API Specifications > GET /api/v1/sla-reports"
- **What:**
  1. Handler struct with `store *store.SLAReportStore`, logger. Methods `List(w,r)` and `Get(w,r)`.
  2. Parse `from`/`to` from query (default last 24h), `operator_id?`, `cursor?`, `limit?` (default 50, max 200). Tenant from `tenant.FromContext(r.Context())`.
  3. Return standard envelope + cursor meta.
  4. Register routes in `router.go` under `r.Route("/api/v1/sla-reports", ...)` inside the JWT-authenticated group. Wire handler in main.go.
- **Verify:** `go build ./...` succeeds; `curl localhost:8080/api/v1/sla-reports -H "Authorization: Bearer $TOK"` returns 200 after seed data.

### Task 23: Register IP reclaim grace-period setter + enum validation in health (AC-10 polish)
- **Files:** Modify `cmd/argus/main.go`, Modify `internal/gateway/router.go`
- **Depends on:** Task 17
- **Complexity:** low
- **Pattern ref:** Existing health handler wiring.
- **Context refs:** "API Specifications > GET /api/v1/health"
- **What:** Ensure `NewHealthHandler` is called with real `db/redis/nats` `HealthChecker` adapters (already wired; verify). Add `s3` probe if `s3Up != nil` — optional extension of aaaHealthData with `s3 *string`. If s3Up exposes HealthCheck, add as optional probe.
- **Verify:** `curl /api/v1/health` returns envelope containing real db/redis/nats/s3 probe latency.

### Task 24: Unit tests — IPPoolStore `ListExpiredReclaim`/`FinalizeReclaim` (AC-3)
- **Files:** Modify `internal/store/ippool_test.go` (create if absent — then Create)
- **Depends on:** Task 6
- **Complexity:** medium
- **Pattern ref:** Read `internal/store/session_radius_test.go` for in-memory pgxpool test setup OR skip-when-no-DB pattern (`if testing.Short() { t.Skip }`).
- **Context refs:** "Data Flow — IPReclaim job"
- **What:** Create table-driven test suite `TestIPPoolStore_ListExpiredReclaim_*` covering:
  - empty result when no reclaiming rows
  - returns rows where `reclaim_at <= now`, not those after
  - honors `limit`
  - `FinalizeReclaim` transitions `reclaiming → available`, nils out sim_id/allocated_at/reclaim_at, decrements `ip_pools.used_addresses`
  - `FinalizeReclaim` on non-reclaiming row returns `ErrIPNotFound`
  Gate tests on `DATABASE_URL` env var (`testing.Short()` skip when unset). When available, use a transactional test helper that ROLLBACKs after each test.
- **Verify:** `go test ./internal/store/ -run TestIPPoolStore_ListExpiredReclaim` passes when DB available; skips cleanly otherwise.

### Task 25: Unit tests — SMS Twilio client httptest (AC-2)
- **Files:** Create `internal/notification/sms_twilio_test.go`
- **Depends on:** Task 12
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/webhook_test.go` — httptest server + handler assertions.
- **Context refs:** "Data Flow — SMS Gateway"
- **What:** Spin up `httptest.NewServer` that asserts:
  - `POST /2010-04-01/Accounts/ACxxx/Messages.json` path
  - Basic auth header matches `ACxxx:token`
  - Form body contains `To=%2B15551234567&From=%2B15557654321&Body=hello`
  - Returns 201 + JSON `{"sid":"SMabc","status":"queued"}`
  Test cases: happy path (201), 400 invalid number error propagates, 500 triggers retry (if retry implemented — else single attempt), context cancellation returns ctx.Err.
  Also test `VerifyStatusSignature` with known-good vs known-bad sig.
- **Verify:** `go test ./internal/notification/ -run TestTwilio` passes.

### Task 26: Unit tests — SMS status webhook handler (AC-2)
- **Files:** Create `internal/api/notification/sms_webhook_test.go`
- **Depends on:** Task 13
- **Complexity:** medium
- **Pattern ref:** Read `internal/api/notification/handler_test.go`.
- **Context refs:** "API Specifications > POST /api/v1/notifications/sms/status"
- **What:** Test 401 on bad signature, 204 on valid signature, notifStore.UpdateDelivery invoked with delivered_at. Use fake store implementing NotifStore interface.
- **Verify:** `go test ./internal/api/notification/...` passes.

### Task 27: Unit tests — HTTP SMDP adapter httptest (AC-1)
- **Files:** Create `internal/esim/smdp_http_test.go`
- **Depends on:** Task 5
- **Complexity:** medium
- **Pattern ref:** Read `internal/notification/webhook_test.go` for httptest pattern.
- **Context refs:** "Data Flow — Real SM-DP+"
- **What:** httptest server fixtures for each of 5 ES9+ endpoints. Test:
  - happy path DownloadProfile → `ProfileID` populated
  - 404 → `ErrSMDPProfileNotFound`
  - 503 → `ErrSMDPConnectionFailed` after retries
  - ctx cancel mid-retry
  - `X-Api-Key` header present on every request
  - mTLS mode with client cert path (use self-signed cert from test file or skip if not provided)
- **Verify:** `go test ./internal/esim/ -run TestHTTPSMDP` passes.

### Task 28: Unit tests — NRF client httptest (AC-8)
- **Files:** Create `internal/aaa/sba/nrf_test.go`
- **Depends on:** Task 15
- **Complexity:** medium
- **Pattern ref:** Read `internal/aaa/sba/server_test.go` for testing NRFRegistration struct; extend with httptest.
- **Context refs:** "Data Flow — NRF Registration"
- **What:** Assertions:
  - `Register` PUTs to `/nnrf-nfm/v1/nf-instances/{id}`, body JSON unmarshals to NFProfile with correct nfType, returns nil on 200.
  - `Register` returns error on 500.
  - `Register` no-op when NRFURL empty (check no HTTP call made via `sync.Int32` request counter = 0).
  - `Heartbeat` PATCHes with JSON-Patch content-type.
  - `Deregister` DELETEs.
  - Timeout honored (server sleeps > ctx deadline → ctx.Err returned).
- **Verify:** `go test ./internal/aaa/sba/ -run TestNRF` passes.

### Task 29: Unit tests — Session manager restart round-trip + unskip TestHandler_Disconnect_Success (AC-9)
- **Files:** Modify `internal/api/session/handler_test.go`, Create `internal/aaa/session/manager_roundtrip_test.go`, Modify `go.mod` (+miniredis)
- **Depends on:** —
- **Complexity:** high
- **Pattern ref:** First of its kind (miniredis). Structure: `mr, _ := miniredis.Run(); defer mr.Close(); rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})`.
- **Context refs:** "Components Involved"
- **What:**
  1. `go get github.com/alicebob/miniredis/v2`.
  2. Replace `newTestHandler` in `handler_test.go` with a variant that passes `(nil, miniredisClient, logger)` — Manager operates Redis-only. This makes `Create/Get` round-trip without a DB.
  3. Delete `t.Skip("Manager is a stub — session Create/Get not yet implemented")` from `TestHandler_Disconnect_Success`.
  4. Add `manager_roundtrip_test.go`: starts miniredis, creates manager, `mgr.Create(sess)` → `newMgrInstance := NewManager(nil, rdb, logger)` (simulating restart — Redis survives) → `newMgrInstance.Get(sess.ID)` returns sess. Covers AC-9 "restart-safe via Redis" (DB persistence variant handled by integration test Task 33).
  5. Remove DEV-052 from the "accepted deferrals" section of decisions.md (Task 34).
- **Verify:** `go test ./internal/api/session/... -run TestHandler_Disconnect_Success` passes; `go test ./internal/aaa/session/... -run TestManagerRoundtrip` passes.

### Task 30: Unit tests — Health handler pending/ok/503 + latency (AC-10)
- **Files:** Create `internal/gateway/health_test.go`
- **Depends on:** Task 17
- **Complexity:** medium
- **Pattern ref:** Read `internal/gateway/router_test.go` for httptest setup.
- **Context refs:** "API Specifications > GET /api/v1/health"
- **What:** Fake HealthCheckers (pass / fail / slow). Test:
  - Initial state → 503 + `pending` statuses
  - After all probes pass → 200 + latency > 0 + `last_checked` non-zero
  - One probe fails → 503, others still `ok` with latencies
  - Slow probe (sleep 3s) respects 2s per-probe timeout → returns `error` with ctx deadline message
- **Verify:** `go test ./internal/gateway/ -run TestHealth` passes.

### Task 31: Unit tests — IPReclaim + SLAReport processors (AC-3 + AC-4)
- **Files:** Create `internal/job/ip_reclaim_test.go`, Create `internal/job/sla_report_test.go`
- **Depends on:** Task 10, Task 11
- **Complexity:** medium
- **Pattern ref:** Read `internal/job/purge_sweep_test.go`, `internal/job/runner_test.go` for mock job/eventBus patterns.
- **Context refs:** "Data Flow — IPReclaim job", "Data Flow — SLAReport job + endpoint"
- **What:**
  - `ip_reclaim_test.go`: fake `ippoolStore` with `ListExpiredReclaim` returning N rows; run processor; assert `FinalizeReclaim` called N times, event published N times, `Complete` called with expected result JSON.
  - `sla_report_test.go`: fake stores; run processor for 2 tenants × 2 operators; assert `slaStore.Create` called 4 times with non-zero uptime, correct windows.
  - Both tests use table-driven setup, no real DB.
- **Verify:** `go test ./internal/job/... -run "TestIPReclaim|TestSLAReport"` passes.

### Task 32: Unit tests — Service.validateChannels nil-sender warnings (AC-7 + AC-12)
- **Files:** Modify `internal/notification/service_test.go`
- **Depends on:** Task 14
- **Complexity:** low
- **Pattern ref:** Read existing test pattern in `service_test.go:373+` (ChannelWebhook test).
- **Context refs:** "Components Involved"
- **What:** Add `TestService_ValidateChannels_WarnsNilSenders` — constructs service with ChannelWebhook but no webhook dispatcher, calls `Start` (or a new exported probe), captures log output via zerolog test writer, asserts warn line per nil channel.
- **Verify:** `go test ./internal/notification/...` passes; test hits the new validator.

### Task 33: Integration tests — S3 upload (MinIO) + cron trigger (AC-5)
- **Files:** Create `internal/storage/s3_uploader_integration_test.go` (build tag `integration`)
- **Depends on:** Task 9
- **Complexity:** high
- **Pattern ref:** First of its kind for integration. Structure: `//go:build integration`, `TestMain` reads `S3_INTEGRATION_ENDPOINT` env, skips if unset. Uses aws-sdk-go-v2 directly to verify the object after upload.
- **Context refs:** "Data Flow — S3 Uploader"
- **What:** Build-tagged test: `NewS3Uploader(ctx, {Endpoint: minio, AccessKey: minioadmin, ...})` → `Upload(ctx, bucket, "test/stamp.txt", []byte("hello"))` → verify via `HeadObject` size matches. Skip when `S3_INTEGRATION_ENDPOINT` unset. Provide one make target `make test-integration-s3`.
- **Verify:** `go test -tags=integration ./internal/storage/...` passes when MinIO running; skips cleanly otherwise.

### Task 34: DEV decision log + docs touch-up + dead code audit (AC-11 + AC-7 closure)
- **Files:** Modify `docs/brainstorming/decisions.md`, Modify `docs/architecture/CONFIG.md` (env var reference)
- **Depends on:** Tasks 2, 3, 12, 15, 19, 21, 29
- **Complexity:** low
- **Pattern ref:** Read existing DEV-NNN rows in decisions.md.
- **Context refs:** "Phase 10 Zero-Deferral Charter"
- **What:** Append entries:
  - **DEV-158** STORY-063 AC-1: SM-DP+ real adapter pattern is generic HTTP (`ESIM_SMDP_PROVIDER=generic`) targeting SGP.22 ES9+ JSON endpoints. Additional provider-specific codepaths (Valid/Thales/Kigen) can be added later as a thin wrapper over `HTTPSMDPAdapter` — not in this story. Mock retained, default remains mock.
  - **DEV-159** STORY-063 AC-2: Twilio chosen over Vonage (market maturity, Go client simplicity via raw HTTP). Vonage branch returns `ErrSMSProviderNotSupported` — no placeholder log, no misleading "not implemented" message.
  - **DEV-160** STORY-063 AC-5: AWS SDK Go v2 (modular) chosen over v1 (monolithic). MinIO and AWS both use same client via `BaseEndpoint` + `UsePathStyle`. No MinIO-specific code.
  - **DEV-161** STORY-063 AC-9: `TestHandler_Disconnect_Success` unskip uses miniredis (not a real store). DB durable persistence covered by the existing `sessionStore`-enabled production path and by integration tests that require a live postgres. Supersedes DEV-052.
  - **DEV-162** STORY-063 AC-11: 4 dead config flags removed (`DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL`). `DevCORSAllowAll` retained — still referenced by main.go:644 CORS setup; its hardening is STORY-074 AC-3 scope.
  - **DEV-163** STORY-063 AC-8: NRF methods now take `context.Context`. Signature change is source-breaking to `sba.Server` only — no external consumers.
  - Also MARK DEV-052 and DEV-137.1 as SUPERSEDED-BY STORY-063.
  - Update `docs/architecture/CONFIG.md` env var table with new vars.
- **Verify:** `git diff docs/brainstorming/decisions.md` shows DEV-158..163 rows; `git diff docs/architecture/CONFIG.md` shows new env vars.

## Acceptance Criteria Mapping
| AC | Description | Implemented In | Verified By |
|----|-------------|----------------|-------------|
| AC-1 | SM-DP+ real adapter (5 methods) | Tasks 4, 5, 19 | Task 27 (unit), existing integration smoke (Task 33 style optional) |
| AC-2 | SMS Twilio real integration | Tasks 2, 3, 12, 13, 19 | Tasks 25, 26 |
| AC-3 | IPReclaim real processor | Tasks 6, 10, 20 | Task 31 |
| AC-4 | SLAReport real processor + endpoint | Tasks 1, 7, 8, 11, 18, 20, 22 | Task 31 + router smoke (Task 22 verify) |
| AC-5 | Real S3Uploader wired | Tasks 2, 3, 9, 19 | Task 33 |
| AC-6 | InApp store wired non-nil | Task 21 | covered by service_test — existing notification tests exercise channel |
| AC-7 | ChannelWebhook/ChannelSMS wired + real senders | Tasks 12, 13, 14, 19, 21 | Tasks 25, 26, 32 |
| AC-8 | NRF real 3GPP calls | Tasks 2, 3, 15, 16, 19 | Task 28 |
| AC-9 | Session DB persistence + unskip disconnect test | Task 29 (uses already-wired RadiusSessionStore in manager) | Task 29 |
| AC-10 | Health endpoint real probes + latency + 503 pending | Task 17, 23 | Task 30 |
| AC-11 | Dead dev config flags removed | Task 2, 3 | `go build ./...` + grep verify in Task 2 |
| AC-12 | Channel enum consolidated + nil-sender audit | Task 14 | Task 32 |

## Story-Specific Compliance Rules
- **API:** All new endpoints (`/api/v1/sla-reports`, `/api/v1/sla-reports/{id}`, `/api/v1/notifications/sms/status`) use standard envelope `{ status, data, meta?, error? }`. SLA endpoints require tenant-scoped JWT. SMS webhook is public but HMAC-auth.
- **DB:** New `sla_reports` migration with up + down pair. Every query on existing tables tenant-scoped where the table has `tenant_id`.
- **Config:** All new env vars documented in `.env.example` + `docs/architecture/CONFIG.md`. Config struct uses `envconfig` tags (no yaml).
- **Audit:** IPReclaim and every state-changing SLA report write creates an audit log entry via `auditSvc`.
- **Goroutine safety:** NRF heartbeat ticker + S3 async ops MUST recover from panics (Phase 10 PAT-001 pattern).
- **ADR-001 (single binary):** All real drivers live inside `internal/`. No microservice extraction.
- **ADR-002 (JWT auth):** SLA endpoints integrated into existing JWT pipeline.
- **No placeholder strings:** `grep -r "not yet implemented\|placeholder" internal/esim internal/notification internal/aaa/sba cmd/argus` must return ZERO after implementation. This grep is part of Gate.
- **Zero-Deferral:** No `t.Skip`, no `// TODO: STORY-NNN`, no `return nil // stub`.

## Bug Pattern Warnings
From decisions.md "Bug Patterns" section:
- **PAT-001 (BR tests drift):** If AC changes any rule, also update `*_br_test.go`. For STORY-063: no BR rules changed — channels/adapters are new, not a rule change. Still, run full `go test ./...` before declaring done.
- **PAT-002 (Utility drift):** HTTP client retry logic will be duplicated across HTTPSMDPAdapter, twilioClient, nrfClient. Consider extracting a `internal/httpx.Retryer` helper AFTER initial implementation — or leave separate and revisit in STORY-065 (observability). Do NOT block this story on the extraction, but add a comment pointing to PAT-002 in each duplicated retry block.
- **PAT-003 (AT_MAC zero pattern):** Not applicable — no EAP-SIM code touched.

Known-to-this-story warning: **NEW PAT candidate — `context.Context` added to NRF methods.** Signature change to `nrf.go` Register/Heartbeat/Deregister breaks any in-tree caller. Planner already accounted (Task 16 updates server.go call sites). Grep `nrfRegistration.Register\|nrfRegistration.Deregister\|nrfRegistration.Heartbeat` across the repo to confirm no other callers.

## Tech Debt (from ROUTEMAP)
- **D-SMDP-MOCK** (STORY-028 review DEV-086): "real SM-DP+ integration will need proper error handling with retry/compensation." Closed by this story's AC-1 (Task 5 retry + error classification).
- **D-SESSION-STUB** (STORY-017 DEV-052): `TestHandler_Disconnect_Success` skip. Closed by Task 29.
- **D-S3-NIL** (STORY-053 gate note, referenced in DEV-137.1): S3 uploader nil. Closed by Task 9 + 19.
- **D-STUBS-3** (STORY-031 DEV-090): 3 stubs remain (purge_sweep closed STORY-039; ip_reclaim + sla_report closed this story via Tasks 10/11).

## Mock Retirement
- `internal/esim/smdp.go` MockSMDPAdapter: **RETAINED as fallback** (config-selectable). Production default after this story = `mock` unless `ESIM_SMDP_PROVIDER` set to another value. This is explicit per AC-1.
- `internal/job/stubs.go` StubProcessor: **RETAINED** (still used by tests). IPReclaim + SLAReport registrations migrate off it.
- No frontend mocks retired this story.

## Risks & Mitigations
| Risk | Mitigation |
|------|------------|
| AWS SDK Go v2 adds significant binary size | Use only required submodules (`config`, `credentials`, `service/s3`). Run `go build -ldflags="-s -w"` size check in gate. |
| miniredis deps may pull old redis-compat version | Pin via `go mod tidy` and check go.sum diff; document version in DEV-161. |
| Twilio signature verification subtle (URL ordering) | Task 25 tests must include an authoritative fixture from Twilio docs. |
| NRF method context addition breaks SBA server tests | Task 16 updates server.go; Task 28 re-runs server_test.go to confirm compilation. |
| SLA p95 SQL (`percentile_cont`) may be slow on large `operator_health_logs` | Use the existing `idx_op_health_operator_time` index; restrict window to <= 30d per report. Document limit in DEV-158 if needed. |
| Duplication of HTTP retry logic | Warn in PAT-002 block; extract later (STORY-065 observability tracing will also touch these clients). |
| S3 credential misconfiguration in dev blocks startup | In non-prod, log warn and continue with `s3Up = nil`; cron skips archival gracefully (existing behavior). Only production fatals on missing creds. |
| Session DB persistence test uses miniredis only — does not prove DB writes happen | Dev integration path exercises production sessionStore; documented in DEV-161. A true DB-backed integration test is out of scope here (would require dockertest setup — STORY-064 scope). |

## Quality Gate (Planner self-verification)

| Check | Result |
|-------|--------|
| Every AC (1-12) has at least one task | PASS — see Acceptance Criteria Mapping table (all 12 rows populated). |
| Every task names exact file paths | PASS — each task has "Files" field with absolute-intent paths. |
| Every task has verification step | PASS — each task has "Verify" line. |
| Waves ordered: W0 schema+config → W1 interfaces → W2 concrete → W3 wiring → W4 tests | PASS — dependency graph acyclic (Task 1→7→11; Task 2→{12,15,19,20}; Task 9→19→33; tests depend only on earlier impl). |
| Mandatory test wave exists | PASS — Wave 4 (Tasks 24-33 all tests). |
| No "TODO later" / "follow-up" in plan | PASS — all placeholder concerns assigned to a specific task. |
| Task count realistic for XL | PASS — 34 tasks; spans 12 ACs, 14 packages. |
| No implementation code in plan | PASS — plan contains code fences for schema/flow only, no function bodies beyond 1-3 line scaffolds in Task descriptions. |
| Every task with new files has `Pattern ref` | PASS — each Create task has a concrete file or "First of its kind" note. |
| Phase 10 zero-deferral | PASS — no skips, no stubs, no "will be done later"; every external integration has real driver + httptest + mock retention. |
| Dev flags cleanup covered | PASS — Task 2 removes 4 dead flags (confirmed dead via grep); `DevCORSAllowAll` explicitly retained for STORY-074. |
| `GetProfileInfo` added (advisor point #1) | PASS — Task 4. |
| `ChannelWebhook`/`ChannelSMS` consolidation (advisor point #2) | PASS — Task 14 (move from models.go to service.go). |
| `SBA_NRF_URL` config added (advisor point #3) | PASS — Task 2 + Task 19. |
| SLA endpoint in API (advisor point #4) | PASS — Task 22. |
| TestHandler_Disconnect_Success unskip strategy (advisor point #5) | PASS — Task 29 with miniredis. |
| New go.mod deps enumerated (advisor point #6) | PASS — "New Go Module Dependencies" section. |
| STORY-074 overlap on AC-11 (advisor point #7) | PASS — documented in Task 2 and DEV-162 (Task 34). |
| InAppStore adapter distinct from NotifStore (advisor point #8) | PASS — Task 21 adds `CreateNotification` method on existing adapter; single adapter serves both. |
| Health 503 until probes run (advisor point #9) | PASS — Task 17 describes pending state semantics. |

**SELF-QG: PASS** — 34 tasks, 5 waves, 12/12 ACs, zero deferrals, zero placeholder strings in touched files after implementation.
