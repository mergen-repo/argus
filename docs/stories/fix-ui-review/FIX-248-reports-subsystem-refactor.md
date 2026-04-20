# FIX-248: Reports Subsystem Refactor — Scope Reduction + 5 New Reports + Local Filesystem Storage

## Problem Statement
**Multiple critical issues with reports subsystem:**

1. **Generation pipeline BROKEN** — `POST /api/v1/reports/generate` submits job; job fails with:
   ```
   scheduled_report: s3 upload: get credentials: failed to refresh cached credentials, 
   no EC2 IMDS role found
   ```
   S3 upload attempts EC2 Instance Metadata Service — absent in Docker local env. No report gets generated.

2. **Scope misalignment** — 8 current reports include:
   - BTK Compliance, KVKK Data Protection, GDPR Compliance (regulatory reports, not needed per DEV-254 compliance page removal)
   - Cost Analysis (not needed per DEV-254 cost page removal)
   - SLA Monthly, Usage Summary, SIM Inventory, Audit Log Export (keep)

3. **Missing M2M-valuable reports** — Fleet Health, Policy Rollout Audit, IP Pool Forecast, CoA Enforcement, Traffic Trend — operational reports absent.

4. **`/reports/scheduled` returns `data: null`** on empty — F-302 / F-328 crash pattern.

User decision (DEV-255, 2026-04-19): Remove 4, keep 4, add 5 = 9 reports total. Replace S3 with Local Filesystem + signed URL endpoint (Option 1).

## User Story
As a platform operator, I want reports to actually generate (currently broken) and cover operational M2M needs (fleet health, rollout audit, capacity forecasts), delivered via downloadable links that work in on-prem / Docker environments without cloud S3 dependency.

## Architecture Reference
- Storage abstraction: `internal/storage/` new interface + LocalFS + S3 impls
- Report builders: `internal/report/` (existing) — add 5 new
- New endpoint: `GET /api/v1/reports/download/{id}?token=<signed>`

## Findings Addressed
- F-325 (generation broken)
- F-326 (scope revision)
- F-327 (Local FS storage)
- F-328 (null response F-302 link)
- F-329 (comprehensive refactor)

## Acceptance Criteria

### Scope Changes (4 remove, 4 keep, 5 add)
- [ ] **AC-1:** **Remove** from definitions + handlers + builders: `compliance_btk`, `compliance_kvkk`, `compliance_gdpr`, `cost_analysis`
- [ ] **AC-2:** **Keep** (no changes to content, but fix generation): `sla_monthly`, `usage_summary`, `sim_inventory`, `audit_log_export`
- [ ] **AC-3:** **Add 5 new reports:**

| ID | Name | Category | Formats | Content |
|----|------|----------|---------|---------|
| `fleet_health` | Fleet Health Report | operational | pdf/csv/xlsx | Active/suspended/terminated breakdown per operator, last_seen distribution histogram, dormant SIM list (>30d no session), orphan/ghost SIM count |
| `policy_rollout_audit` | Policy Rollout Audit | operational | pdf/csv | Per-policy rollout history: who started, duration, stages completed, success rate, failure reasons, CoA ack summary |
| `ip_pool_forecast` | IP Pool Capacity Forecast | operational | pdf/csv | Per-pool utilization trend (last 30d), exhaustion projection (linear + 7d-avg), pools approaching capacity alert |
| `coa_enforcement` | CoA Enforcement Report | operational | csv | Policy change CoA ack %, per-operator breakdown, failed delivery list with reasons, retry outcomes |
| `traffic_trend` | Traffic Trend Report | analytics | pdf/csv | Peak hour analysis (heatmap data), top 50 consumer SIMs, anomaly correlations, MoM/WoW change |

### Storage Refactor (S3 → Local FS abstraction)
- [ ] **AC-4:** New `internal/storage/storage.go` interface:
  ```go
  type Storage interface {
      Put(ctx, key, content, contentType) error
      Get(ctx, key) ([]byte, error)
      SignedURL(ctx, key, ttl) (url, err)
      Delete(ctx, key) error
  }
  ```
- [ ] **AC-5:** **LocalFS implementation** (default):
  - Env: `REPORT_STORAGE=local` (default), `REPORT_STORAGE_PATH=/var/lib/argus/reports`, `REPORT_SIGNING_KEY=<32-byte-hex>`, `REPORT_RETENTION_DAYS=90`
  - File path: `{base}/{year}/{month}/{day}/{report_id}.{ext}` (hierarchical for cleanup efficiency)
  - Signed URL: `/api/v1/reports/download/{id}?token=<HMAC-SHA256(id|expire_timestamp, key)>`
  - TTL 7 days (matches current S3 behavior)
- [ ] **AC-6:** **S3 implementation preserved** (optional for cloud deploy):
  - Env: `REPORT_STORAGE=s3` opt-in
  - Existing S3 upload code behind this flag
  - Current `nullReportStorage` wrapper usage replaced with actual switch
- [ ] **AC-7:** **Download endpoint**:
  - Route: `GET /api/v1/reports/download/{report_id}?token=<signed>`
  - Handler: verify HMAC token + expiration → stream file via `io.Copy(w, f)` with Content-Disposition
  - File not found: 404
  - Invalid/expired token: 401
  - Content-Type set per extension (application/pdf, text/csv, application/vnd.openxmlformats-officedocument.spreadsheetml.sheet)
- [ ] **AC-8:** **Docker integration:**
  - `deploy/docker-compose.yml` adds volume: `./data/reports:/var/lib/argus/reports`
  - `.env.example` adds REPORT_STORAGE* entries

### Cleanup Cron
- [ ] **AC-9:** New job processor `internal/job/report_cleanup.go`:
  - Runs daily (2 AM)
  - Walks `REPORT_STORAGE_PATH`, deletes files with mtime > retention
  - Logs summary: "Deleted N files, reclaimed M bytes"
- [ ] **AC-10:** Cron entry added to `cmd/argus/main.go` scheduler (`report_cleanup` type)

### Report Builders (5 new)
- [ ] **AC-11:** `internal/report/fleet_health.go`:
  - Query: sims grouped by state + last_seen histogram
  - Output: PDF has summary page + per-operator table; CSV has one row per SIM with last_seen
- [ ] **AC-12:** `internal/report/policy_rollout_audit.go`:
  - Query: policy_rollouts JOIN audit_logs for history
  - Output: PDF timeline per policy; CSV row per rollout
- [ ] **AC-13:** `internal/report/ip_pool_forecast.go`:
  - Query: ip pool utilization time series + linear regression for projection
  - Output: PDF has chart per pool + exhaustion table; CSV raw forecast data
- [ ] **AC-14:** `internal/report/coa_enforcement.go`:
  - Query: policy_assignments.coa_status distribution + enrichment
  - Output: CSV detail per assignment with CoA outcome
- [ ] **AC-15:** `internal/report/traffic_trend.go`:
  - Query: analytics usage aggregates (hourly/daily)
  - Output: PDF heatmap + top consumers; CSV time series

### Bug Fixes
- [ ] **AC-16:** `/reports/scheduled` empty response returns `{"data": [], ...}` not `data: null` (F-302 global fix via FIX-241 — this story confirms)
- [ ] **AC-17:** FE reports page list handles empty gracefully after FIX-241

### Tests
- [ ] **AC-18:** Integration test per report type: generate → artifact created → signed URL → download endpoint streams → file matches expected format
- [ ] **AC-19:** Storage swap test: `REPORT_STORAGE=local` + `=s3` both work under same handler flow (mock S3 for test)
- [ ] **AC-20:** Cleanup test: seed old files → run cleanup → verify deleted

### Docs
- [ ] **AC-21:** `docs/architecture/REPORTS.md` (NEW) — list of 9 reports, formats, example outputs, signed URL contract
- [ ] **AC-22:** `docs/architecture/CONFIG.md` — REPORT_STORAGE* env variables

## Files to Touch
- **Backend:**
  - `internal/storage/` (NEW package)
  - `internal/report/` — remove 4, add 5 builder files
  - `internal/api/reports/handler.go` — scope update, download endpoint
  - `internal/job/scheduled_report.go` — use storage interface
  - `internal/job/report_cleanup.go` (NEW)
  - `cmd/argus/main.go` — wire storage backend based on env, cleanup cron
- **Frontend:**
  - `web/src/pages/reports/index.tsx` — updated report definitions list
  - `web/src/hooks/use-reports.ts` — download link construction
- **Config:**
  - `deploy/docker-compose.yml` volume mount
  - `.env.example`
- **Docs:** `REPORTS.md`, `CONFIG.md`

## Risks & Regression
- **Risk 1 — Existing S3-stored reports inaccessible:** Not applicable — reports were never generating successfully. No data to preserve.
- **Risk 2 — Multi-instance deploy:** Local FS requires shared volume. Document in `DEPLOYMENT.md`.
- **Risk 3 — Cleanup cron deletes active downloads:** File deleted mid-stream? Mitigation: download endpoint grabs file handle before send; delete only on mtime > retention (by then no active token valid).
- **Risk 4 — Signed URL replay:** Token single-use? Or time-bound only? Time-bound simpler, acceptable for internal admin tool. Document.
- **Risk 5 — PDF generation dependency:** wkhtmltopdf or chromedp? Decide + document in `docs/architecture/DEPENDENCIES.md`.

## Test Plan
- Per-report integration test (5 new + 4 existing) — end-to-end generate + download
- Storage swap test — LocalFS + S3 (mocked)
- Cleanup test — file eviction by age
- Browser: reports page lists 9 definitions; generate + download each

## Plan Reference
Priority: P1 · Effort: XL · Wave: 9 · Depends: FIX-241 (nil-slice — scheduled_reports list)
