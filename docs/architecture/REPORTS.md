# Reports — Subsystem Overview (FIX-248)

The reports subsystem generates point-in-time summaries (PDF / CSV / XLSX)
from the Argus operational data and serves them to operators via signed
download URLs. This document is the home for the **current report
catalogue**, the **storage abstraction**, and the **signed URL contract**.

Maintenance: every story that adds, removes, or renames a report MUST
update the catalogue table below in the same commit.

---

## 1. Report catalogue

| ID | Name | Category | Formats | Builder file |
|----|------|----------|---------|--------------|
| `sla_monthly` | Monthly SLA Report | operational | pdf / csv / xlsx | `internal/report/store_provider.go::SLAMonthly` |
| `usage_summary` | Usage Summary | analytics | pdf / csv / xlsx | `internal/report/store_provider.go::UsageSummary` |
| `audit_log_export` | Audit Log Export | security | csv / xlsx | `internal/report/store_provider.go::AuditExport` |
| `sim_inventory` | SIM Inventory Report | operational | csv / xlsx | `internal/report/store_provider.go::SIMInventory` |

Removed by FIX-248 (DEV-560): `compliance_btk`, `compliance_kvkk`,
`compliance_gdpr` (regulatory reports — compliance page deprecated per
DEV-254), `cost_analysis` (cost page deprecated). Underlying builder Go code
stays in tree until D-166 deletes it atomically with the new builders
landing under D-165.

### Future catalogue (D-165, planned)

The 5 builders below were specified in FIX-248 AC-3 but deferred to D-165
(each is a 200+ line query + renderer pair; a dedicated story per builder
is the right unit of work).

| ID | Name | Category | Formats | Content sketch |
|----|------|----------|---------|----------------|
| `fleet_health` | Fleet Health Report | operational | pdf / csv / xlsx | active/suspended/terminated breakdown per operator; last_seen histogram; dormant SIM list (>30d no session); orphan/ghost SIM count |
| `policy_rollout_audit` | Policy Rollout Audit | operational | pdf / csv | per-policy rollout history (initiator, duration, stages, success rate, failure reasons, CoA ack summary) |
| `ip_pool_forecast` | IP Pool Capacity Forecast | operational | pdf / csv | per-pool utilisation trend + linear/7d-avg exhaustion projection + alert list |
| `coa_enforcement` | CoA Enforcement Report | operational | csv | policy-change CoA ack %, per-operator breakdown, failed-delivery list, retry outcomes |
| `traffic_trend` | Traffic Trend Report | analytics | pdf / csv | peak-hour heatmap, top 50 consumer SIMs, anomaly correlations, MoM/WoW change |

---

## 2. Storage abstraction

`internal/storage/storage.go` defines:

```go
type Storage interface {
    Upload(ctx, bucket, key, data) error
    PresignGet(ctx, bucket, key, ttl) (url, error)
}
```

Two implementations satisfy the interface and are interchangeable:

| Backend | When | Notes |
|---------|------|-------|
| `LocalFSUploader` | `REPORT_STORAGE=local` (default) | Disk-backed; HMAC-signed download URL; suitable for single-instance deployments and Docker dev |
| `S3Uploader` | `REPORT_STORAGE=s3` | Cloud deployments; the S3-presigned URL is served directly to the FE — the download endpoint is **not** registered in this case |

`bucket` is retained as a parameter for S3 backwards compat; LocalFS ignores it.

### LocalFS path layout

```
{REPORT_STORAGE_PATH}/tenants/{tenant}/reports/{job}/{filename}
```

### Backend selection

```
selectReportStorage(cfg, s3Impl, logger) storage.Storage
   REPORT_STORAGE=s3   → S3Uploader (errors if S3 not configured; falls back to LocalFS)
   REPORT_STORAGE=local → LocalFSUploader (default)
```

---

## 3. Signed URL contract

```
{REPORT_PUBLIC_BASE_URL}/api/v1/reports/download/{key_b64}?expires={unix}&sig={hex}

  key_b64 = base64.RawURLEncoding(key)
  sig     = hex(HMAC-SHA256(key + "|" + expires_unix, REPORT_SIGNING_KEY))
  TTL     = 7 days
```

The FE renders an `<a href>` directly to this URL — there is no JWT header
on the download request because browser `<a>` clicks can't carry custom
auth headers. The HMAC token is the auth.

The download handler (`internal/api/reports/download.go`):

1. Decodes `key_b64` (rejects `..` and absolute paths)
2. Parses `expires`; rejects past timestamps
3. Re-derives the HMAC with the configured signing key; constant-time compare
4. Opens the file from the LocalFS backend; streams via `io.Copy`
5. Sets `Content-Type` per extension (`application/pdf`, `text/csv`, etc.)
6. Returns:
   - `200` + body on success
   - `401` for invalid / expired / missing tokens
   - `404` for missing files (file existed but was retention-cleaned, or wrong key)
   - `503` if the LocalFS backend isn't configured (e.g. running with `REPORT_STORAGE=s3`)

Multi-instance deploys: every instance MUST share the same
`REPORT_SIGNING_KEY`, otherwise a URL minted by instance A doesn't verify
on instance B. The boot log emits a warning when the key is auto-generated.

---

## 4. Retention

LocalFS files are subject to `REPORT_RETENTION_DAYS` (default 90). The
cleanup cron processor (D-167 — deferred from FIX-248 inline scope) walks
the storage path daily at 02:00 UTC and deletes files older than retention.
S3 backend retention is left to S3 lifecycle policies.

The 7-day signed-URL TTL plus the 90-day file retention leave an 83-day
gap; download tokens always expire before the file does, so a successful
download token implies the file is still present.

---

## 5. Environment variables (quick reference — full list in CONFIG.md)

```
REPORT_STORAGE=local                 # local | s3
REPORT_STORAGE_PATH=/var/lib/argus/reports
REPORT_SIGNING_KEY=<32-byte-hex>     # auto-generated on boot if empty (warning logged)
REPORT_RETENTION_DAYS=90
REPORT_PUBLIC_BASE_URL=http://localhost:8084
```

See `docs/architecture/CONFIG.md` § "Report Storage" for the canonical
descriptions.

---

## 6. Out of scope (deferred)

- **D-165** — 5 new operational reports (fleet_health, policy_rollout_audit,
  ip_pool_forecast, coa_enforcement, traffic_trend). Each is a builder + tests
  pair; one story per builder is the right unit.
- **D-166** — atomic deletion of dead-code KVKK / GDPR / BTK / CostAnalysis
  builder Go files. The validation map and FE list already drop them
  (FIX-248); the source files remain to land in one git diff with the
  corresponding D-165 additions.
- **D-167** — cleanup cron processor (`internal/job/report_cleanup.go`) that
  walks `REPORT_STORAGE_PATH` and evicts files past retention. Documented
  here; implementation deferred from FIX-248 inline scope.
- AC-18..AC-20 deeper integration tests deferred along with the new
  builders (D-165).

---

*Last updated 2026-04-27 — FIX-248 DEV-564.*
