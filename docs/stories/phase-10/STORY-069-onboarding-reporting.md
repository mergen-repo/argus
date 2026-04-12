# STORY-069: Onboarding, Reporting & Notification Completeness

## User Story
As an enterprise customer and compliance officer, I want a real end-to-end onboarding wizard, scheduled report generation in PDF/CSV/Excel, webhook delivery tracking with HMAC signatures, per-tenant locale-aware notifications, full GDPR data portability export, and auto-scheduled KVKK purge, so that Argus can be sold to enterprise/regulated customers without last-mile gaps.

## Description
Audit surfaced multiple incomplete enterprise workflows: onboarding wizard E2E test exists but UI + backend orchestration is manual multi-step, scheduled reports are spec'd but not implemented (handleGenerate is a 2-second setTimeout), webhooks have no delivery tracking or HMAC verification, notification templates are English-only, GDPR data portability coverage is unclear, KVKK purge requires manual cron trigger, and IP grace-period release is not scheduled. Close all of them.

> Updated by Compliance Auditor [2026-04-12]: added AC-12 from audit gap FEAT-055 / API-170 / API-171 (SMS Gateway outbound) — see docs/reports/compliance-audit-report.md.

## Architecture Reference
- Services: SVC-03 (Core API), SVC-07 (Analytics — reports), SVC-08 (Notification), SVC-09 (Job Runner — scheduled reports), SVC-10 (Audit — compliance)
- Packages: internal/api/onboarding, internal/api/reports, internal/notification, internal/api/compliance, internal/job, web/src/pages/onboarding, web/src/pages/reports, migrations
- Source: Phase 10 business logic audit (6-agent scan 2026-04-11), STORY-055 (onboarding E2E), STORY-039 (compliance), STORY-038 (notification)

## Screen Reference
- SCR-030..034 (Onboarding Wizard — 5 steps)
- SCR-125 (Compliance Reports List), SCR-126 (Generate Report dialog), SCR-127 (Scheduled Reports)
- SCR-110 (Notification Channels), SCR-111 (Notification Preferences — per-tenant + locale), SCR-112 (Notification Templates)

## Acceptance Criteria
- [ ] AC-1: **Onboarding wizard end-to-end.**
  - 5-step flow: (1) Company info, (2) Admin user, (3) Operator grants, (4) Initial APN, (5) Import SIMs (CSV upload)
  - State persisted server-side (`onboarding_sessions` table) so user can resume after disconnect
  - Each step validates and commits atomically; failure on step N does not leave partial state
  - Final step triggers post-onboarding setup: default policy assignment, welcome notification, sample dashboard
  - UI shows step-by-step progress, back navigation, summary review before final submit
  - Backend endpoints: `POST /onboarding/start`, `POST /onboarding/:id/step/:n`, `GET /onboarding/:id`, `POST /onboarding/:id/complete`
- [ ] AC-2: **Scheduled report generation.**
  - `scheduled_reports` table (`id`, `tenant_id`, `report_type`, `schedule_cron`, `format`, `recipients[]`, `last_run_at`, `next_run_at`, `state`)
  - Cron job runs scheduled reports, generates file, uploads to S3 (via STORY-063 AC-5), emails link or attaches file
  - API: `POST /reports/scheduled` (create), `GET /reports/scheduled`, `PATCH/:id`, `DELETE/:id`
  - Frontend Reports page wires real API (replaces hardcoded arrays + fake setTimeout from STORY-070)
- [ ] AC-3: **On-demand report generation.**
  - `POST /reports/generate` with `{report_type, format, filters}` returns job_id (async if large) or file bytes (small)
  - Tracked in jobs table if async
  - Frontend shows progress bar, download button when ready
- [ ] AC-4: **Report formats: PDF, CSV, Excel.**
  - PDF via `github.com/go-pdf/fpdf` (or similar) — formatted with tenant branding, charts as embedded images
  - CSV — already supported, verify all report types
  - Excel (.xlsx) via `github.com/xuri/excelize/v2` — multi-sheet support for complex reports
  - Report types: `compliance_kvkk`, `compliance_gdpr`, `compliance_btk`, `sla_monthly`, `usage_summary`, `cost_analysis`, `audit_log_export`, `sim_inventory`
- [ ] AC-5: **Webhook delivery tracking.**
  - `webhook_deliveries` table (`id`, `tenant_id`, `config_id`, `event_type`, `payload_hash`, `response_status`, `response_body`, `attempt_count`, `next_retry_at`, `final_state`, `created_at`)
  - Every delivery attempt logged
  - Frontend webhook config shows recent deliveries + retry button
  - Exponential backoff retry (max 5 attempts over 1h)
  - Dead-letter queue after max attempts (logged, surfaced in UI)
- [ ] AC-6: **Webhook HMAC signature.**
  - Outbound: `X-Argus-Signature: sha256=<hmac>` header, signed with webhook secret
  - Verification script provided in docs
  - Inbound (if any inbound webhooks): verify `X-Provider-Signature` before processing
- [ ] AC-7: **Per-tenant notification preferences.**
  - `notification_preferences` table (`tenant_id`, `event_type`, `channels[]`, `severity_threshold`, `enabled`)
  - Frontend Notification Channels page adds preferences matrix (rows: event types, cols: channels, cells: enable/disable + severity)
  - Dispatch logic reads preferences before sending; if event not enabled for any channel, skip
- [ ] AC-8: **Locale-aware notification templates.**
  - `notification_templates` table (`event_type`, `locale`, `subject`, `body_text`, `body_html`)
  - Supported locales: `tr`, `en` (default fallback to `en` if locale missing)
  - User locale stored in `users.locale` column (existing or new)
  - Dispatch looks up template by `(event_type, user.locale)`, renders with event payload
- [ ] AC-9: **GDPR data portability export.**
  - `POST /api/v1/compliance/data-portability/:user_id` — exports all personal data for the user
  - Coverage: user profile, tenant metadata, owned SIMs, session history (last 90d), CDRs (last 90d per retention), audit logs where user is actor
  - Format: JSON (machine-readable) + human-readable PDF summary
  - Delivered via async job + S3 signed URL email
  - Compliance: KVKK 11. madde + GDPR Article 20 coverage
- [ ] AC-10: **KVKK auto-purge scheduler.**
  - `cron_kvkk_purge @daily` runs automatically (existing mechanism)
  - Reads `tenants.purge_retention_days` OR `tenant_retention_config` per-tenant override
  - Purges (pseudonymizes) expired PII from cdrs, sessions, user_sessions, audit_logs.details where tenant retention exceeded
  - Dry-run mode (`--dry-run` flag) reports what would be purged without executing
  - Audit log entry for each purge run
- [ ] AC-11: **IP grace period release scheduler.**
  - `cron_ip_grace_release @hourly` runs automatically
  - Finds `ip_addresses` where `released_at IS NULL AND grace_expires_at < NOW()` and sim state is terminated
  - Sets state=available, grace_expires_at=NULL, emits NATS `ip.released` event
  - Metric `argus_ip_grace_released_total` increments
- [ ] AC-12: **[AUDIT-GAP] SMS Gateway outbound endpoints (F-055 partial coverage closure).**
       - Source: docs/reports/compliance-audit-report.md (API-170, API-171, F-055)
       - Added by: Compliance Auditor [2026-04-12]
       - Implement `POST /api/v1/sms/send` (API-170): JWT (sim_manager+), request `{ sim_id, text, priority? }`, sends device-management SMS via configured SMS provider (reuse STORY-063 Twilio adapter), returns `{ message_id, queued_at }`. Enforces per-tenant rate limit (`RATE_LIMIT_SMS_PER_MINUTE` env var). Audit log entry emitted (`action=sms.sent`).
       - Implement `GET /api/v1/sms/history` (API-171): JWT (sim_manager+), cursor-paginated, query filters `sim_id`, `from`, `to`, `status` (queued/sent/delivered/failed). Returns list of SMS records from `sms_outbound` table.
       - New table `sms_outbound` (migration): `id`, `tenant_id`, `sim_id`, `msisdn`, `text_hash`, `status`, `provider_message_id`, `error_code`, `queued_at`, `sent_at`, `delivered_at`, indexed by `(tenant_id, sim_id, queued_at DESC)`.
       - Delivery callback reuses existing `POST /api/v1/notifications/sms/status` webhook handler (API-185) but matches against both inbound-notification and outbound-gateway records by `provider_message_id`.
       - Feature F-055 marked COVERED in PRODUCT → Story matrix after this AC lands.

## Dependencies
- Blocked by: STORY-059 (PDF export base), STORY-063 (S3 uploader + SMS + InApp real), STORY-065 (job metrics)
- Blocks: Phase 10 Gate

## Test Scenarios
- [ ] E2E: New user goes through onboarding wizard from scratch → tenant + admin + operator + APN + SIM import all committed. Resume after browser refresh mid-step works.
- [ ] Integration: Create scheduled report `daily_usage_summary @ 02:00 format=pdf` → cron triggers at mock time → PDF in S3 + email sent.
- [ ] Integration: Generate KVKK compliance report PDF → download → open → sections (data subjects, retention, purges, access log) present with real data.
- [ ] Integration: Webhook delivery fails with 500 → retried 4 more times with exp backoff → marked dead-letter → visible in UI.
- [ ] Integration: POST signed webhook to test endpoint → receiver verifies HMAC → valid signature passes.
- [ ] Integration: User locale=tr → welcome notification email contains Turkish subject + body. User locale=en → English.
- [ ] Integration: GDPR portability request → async job starts → S3 URL emailed → downloaded JSON contains all personal data.
- [ ] Integration: KVKK auto-purge cron runs → expired CDRs pseudonymized, audit entry created.
- [ ] Integration: Terminate SIM → grace timer → cron sweeper releases IP → NATS event emitted.

## Effort Estimate
- Size: L
- Complexity: Medium-High (many features, but each is well-scoped)
