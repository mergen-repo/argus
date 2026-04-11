# STORY-063: Backend Implementation Completeness

## User Story
As a platform engineer, I want every stub, mock adapter, nil wire, and placeholder log in the production code path replaced with real implementations, so that Argus behaves correctly end-to-end for every spec'd feature — not just the happy development path.

## Description
Comprehensive backend audit surfaced ~10 silently-broken features where code compiles and tests pass but production behavior is fake: eSIM provisioning calls a mock adapter, job processors return `{"status":"stub"}`, S3 uploader is `nil`, session state lives only in Redis, NRF registration logs placeholders, SMS channels return "not yet implemented", InApp notifications are wired as `nil`, health endpoint returns hardcoded "ok". Close every one of these in a single disciplined pass.

## Architecture Reference
- Services: SVC-02 (WebSocket), SVC-03 (Core API), SVC-04 (AAA — 5G SBA), SVC-06 (Operator — eSIM), SVC-07 (Analytics — S3 archival), SVC-08 (Notification), SVC-09 (Job Runner)
- Packages: internal/esim (SM-DP+), internal/notification (SMS + InApp), internal/job (IPReclaim, SLAReport, S3Archival), internal/aaa/sba (NRF), internal/aaa/session (DB persistence), internal/gateway/health, cmd/argus/main.go
- Source: Phase 10 comprehensive audit (6-agent scan 2026-04-11), STORY-028 DEV-086, STORY-031 job runner scope

## Screen Reference
- SCR-072 (eSIM Profiles — real provisioning UX)
- SCR-110 (Notification Channels — SMS active, InApp visible)
- SCR-120 (System Health — real dependency probes)
- SCR-070 (Live Sessions — restart-safe)

## Acceptance Criteria
- [ ] AC-1: **SM-DP+ real adapter** implemented against SGP.22 ES9+ protocol. At minimum one real provider adapter (e.g., Valid, Thales, IDEMIA test sandbox, or Kigen) plus preserved mock for dev. Config var `ESIM_SMDP_PROVIDER=mock|valid|thales|...`. Adapter interface covers: `DownloadProfile`, `EnableProfile`, `DisableProfile`, `DeleteProfile`, `GetProfileInfo`, with proper error propagation, retry/compensation, and timeout.
- [ ] AC-2: **SMS gateway real integration.** Twilio OR Vonage (one is enough for v1) fully implemented in `internal/notification/sms.go`. Credentials via env vars, delivery status tracked via webhook callback, rate limit applied. Mock fallback retained for tests.
- [ ] AC-3: **IPReclaim job real.** Replace `NewStubProcessor(JobTypeIPReclaim, ...)` with real processor that (a) enumerates IP addresses past grace period after SIM termination, (b) releases them back to the pool, (c) emits NATS `ip.reclaimed` event, (d) logs audit. Tested with fixture of terminated SIMs.
- [ ] AC-4: **SLAReport job real.** Replace stub with real processor that aggregates operator health logs + session stats into per-tenant SLA summary (uptime %, latency p95, incident count, MTTR). Writes to `sla_reports` table (new migration). Exportable via new endpoint.
- [ ] AC-5: **S3Uploader instantiation.** In `cmd/argus/main.go`, replace `var s3Uploader job.S3Uploader` (nil) with real AWS SDK or S3-compatible uploader (MinIO for on-prem). Config: `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_REGION`, `S3_PATH_STYLE`. S3ArchivalProcessor verified to actually upload CDR archives and audit log exports. Smoke test: run cron once, verify object exists in bucket.
- [ ] AC-6: **InApp notification store wired.** Construct `notification.NewService(emailSender, telegramSender, inAppStore, ...)` with real InApp store (backed by `notifications` table). Third parameter no longer nil. In-app notification badge + notification center fed from this store. STORY-057 AC-7 (unread-count) depends on this.
- [ ] AC-7: **Dead channel constants resolved.** `ChannelWebhook` and `ChannelSMS` either (a) defined as proper constants and wired through `dispatchToChannels` with working sender, or (b) removed entirely if out-of-scope. Decision: define both with real senders (SMS via AC-2, Webhook already exists in STORY-038 — wire it properly).
- [ ] AC-8: **5G SBA NRF real registration.** `internal/aaa/sba/nrf.go` `Register`, `Deregister`, `Heartbeat`, `NotifyStatus` methods make real 3GPP NRF API calls (via NFRegister/NFUpdate) when `SBA_NRF_URL` is set. Placeholder logs removed. Mock retained for when URL is empty (dev).
- [ ] AC-9: **Session manager DB persistence.** `internal/aaa/session/manager.go` writes session create/update/end to `sessions` table alongside Redis cache. Redis remains primary read path, DB is durable source of truth. Restart test: create session → kill app → restart → session queryable. `TestHandler_Disconnect_Success` unskipped (previously deferred from STORY-017).
- [ ] AC-10: **Health endpoint real probes.** `internal/gateway/health.go` probes actually return real status from Ping(ctx) calls. Initial values not "ok" — return 503 if check was never run. Expose probe latency in response. Separate endpoints prepared for STORY-066 liveness/readiness split.
- [ ] AC-11: Dev-only config flags (`DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL`) either (a) consumed by real code paths, or (b) removed from config struct and `.env.example`. Current dead declarations removed.
- [ ] AC-12: Missing `ChannelWebhook`/`ChannelSMS` enum values added to `internal/notification/service.go` constants. Tests verify every channel has a non-nil sender or explicit nil-skip with log warning.

## Dependencies
- Blocked by: STORY-056 (runtime fixes), STORY-057 (API-052 for SLA report structure)
- Blocks: STORY-061 (eSIM multi-profile — needs real SM-DP+), STORY-064 (session DB coverage feeds into audit query tests)

## Test Scenarios
- [ ] Integration: Download eSIM profile via real SM-DP+ sandbox → profile enabled on SIM, ICCID returned, audit entry created.
- [ ] Integration: Send SMS via Twilio test number → delivery status webhook received → notification row updated.
- [ ] Integration: Terminate SIM → wait grace period (mocked clock) → run IPReclaim cron → IP released, `ip_addresses.state=available`.
- [ ] Integration: Run SLAReport cron → `sla_reports` row created with non-zero values.
- [ ] Integration: Trigger `cdr_archival` job → S3 bucket contains `cdrs/YYYY/MM/DD.parquet` (or .csv.gz) file.
- [ ] Integration: POST notification with channel=in_app → notifications table row exists → badge count increments.
- [ ] Integration: 5G SBA startup with `SBA_NRF_URL=http://mock-nrf` → NRF POST /nf-instances received, 200 response, periodic heartbeat observed.
- [ ] Integration: Create RADIUS session → `sessions` table row exists → kill app → restart → `GET /sessions/:id` returns session.
- [ ] Integration: `GET /api/health` with DB down → returns 503 with db.status=error, db.latency_ms>0.
- [ ] Unit: Every channel constant in `notification.Channel` has a registered sender or explicit nil-skip in dispatch.

## Effort Estimate
- Size: XL
- Complexity: High (10 independent real-integrations, some need external sandbox accounts)
