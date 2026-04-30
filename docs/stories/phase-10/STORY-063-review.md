# Review Report — STORY-063: Backend Implementation Completeness

> Reviewer: Amil Reviewer Agent
> Date: 2026-04-12
> Gate result: PASS (1859 tests, 0 failures)
> ACs closed: 12/12

---

## Impact Table

| Area | Change | Scope |
|------|--------|-------|
| SM-DP+ | `HTTPSMDPAdapter` — real SGP.22 ES9+ HTTP calls for DownloadProfile / Enable / Disable / Delete | `internal/esim/smdp.go` |
| SMS | Twilio integration via raw HTTP (no SDK), HMAC-SHA256 webhook callback | `internal/notification/` |
| Job: IPReclaim | Real processor: enumerates expired IPs, releases to pool, emits `ip.reclaimed` NATS, audit log | `internal/job/` |
| Job: SLAReport | Real processor: aggregates operator health + session stats, writes to TBL-27, emits `sla.report.generated` | `internal/job/` |
| S3 Uploader | aws-sdk-go-v2 (modular); same client for AWS and MinIO via `BaseEndpoint + UsePathStyle` | `internal/analytics/` |
| InApp notification | Wired to real `notification.NewService` constructor (no longer nil) | `internal/notification/` |
| Channel constants | `ChannelWebhook` and `ChannelSMS` wired (no longer dead) | `internal/notification/` |
| NRF | Real 3GPP NFRegister/NFUpdate/NFDeregister/NFStatusSubscribe HTTP calls when `SBA_NRF_URL` set | `internal/aaa/` |
| Session persistence | `sessions` table written alongside Redis cache | `internal/aaa/` |
| Health probes | Real probes with latency reporting; returns 503 if no probe has run | `internal/gateway/` |
| Dead config flags | `DevSeedData`, `DevMockOperator`, `DevDisable2FA`, `DevLogSQL` removed (4 flags) | `internal/config/` |
| New env vars | 13 new vars: 5 ESIM, 5 SMS/Twilio, 3 SBA NRF | `internal/config/` |
| New table | TBL-27 `sla_reports` (migration `20260412000001`) | `migrations/` |
| New endpoints | API-183 GET /sla-reports, API-184 GET /sla-reports/{id}, API-185 POST /notifications/sms/status | `internal/api/` |
| Test harness | `alicebob/miniredis/v2` — in-memory Redis for tests | `internal/*/` tests |

---

## Documents Updated

| Document | Change | Status |
|----------|--------|--------|
| `docs/architecture/api/_index.md` | Added API-183, API-184, API-185; updated footer 108→111 | FIXED |
| `docs/architecture/db/_index.md` | Added TBL-27 sla_reports row; updated AAA & Analytics domain to include TBL-27 | FIXED |
| `docs/GLOSSARY.md` | NRF: removed "placeholder" — real 3GPP calls documented; SM-DP+ Adapter: removed "future extension" — HTTPSMDPAdapter documented | FIXED |
| `docs/ARCHITECTURE.md` | Header updated: 108→111 APIs, 24→27 tables | FIXED |
| `docs/USERTEST.md` | Added STORY-063 section with 8 backend verification steps | FIXED |
| `docs/ROUTEMAP.md` | STORY-063 marked DONE (2026-04-12); counter 5/22→6/22; Change Log entry added | FIXED |

---

## Cross-Doc Consistency

| Check | Result |
|-------|--------|
| API index vs story ACs | PASS — API-183/184/185 now present and match story spec |
| DB index vs migration | PASS — TBL-27 sla_reports added; migration `20260412000001` referenced |
| CONFIG.md env vars | PASS — all 13 new vars present (eSIM, SMS, SBA NRF sections) |
| WEBSOCKET_EVENTS.md | PASS — `ip.reclaimed` / `sla.report.generated` are internal NATS events, no client WS event schema needed |
| Dead config flags | PASS — `DevCORSAllowAll` intentionally kept (scope: STORY-074); 4 removed correctly |

---

## Decision Tracing

| Decision | Status | Summary |
|----------|--------|---------|
| DEV-158 | ACCEPTED | Generic HTTP SM-DP+ adapter (no operator-specific SDK coupling) |
| DEV-159 | ACCEPTED | Twilio via raw HTTP (no Twilio Go SDK — avoids mandatory dependency) |
| DEV-160 | ACCEPTED | aws-sdk-go-v2 modular; same client for AWS S3 and MinIO (BaseEndpoint + UsePathStyle) |
| DEV-161 | ACCEPTED | miniredis for in-memory Redis in test harness (replaces flaky real-Redis test pattern) |
| DEV-162 | ACCEPTED | Session persistence: write to `sessions` table alongside Redis cache |
| DEV-163 | ACCEPTED | Health endpoint: 503 if no probe has run yet (fail-safe, not fail-open) |
| DEV-052 | SUPERSEDED by DEV-161 | Original Redis test strategy |
| DEV-137.1 | SUPERSEDED by DEV-160 | Original S3 strategy |

---

## USERTEST Completeness

| Check | Result |
|-------|--------|
| USERTEST.md has STORY-063 section | FIXED — added in this review |
| Steps cover all key deliverables | PASS — health probe 503, TBL-27 query, API-183/184 curl, SM-DP+ env check, NRF NFRegister log, session DB write, make test |

---

## Tech Debt Pickup

| ID | Target | Status |
|----|--------|--------|
| None | — | No existing tech debt items targeted STORY-063 |

---

## Issues Table

| # | Finding | Severity | Status |
|---|---------|----------|--------|
| 1 | `api/_index.md` missing API-183, API-184, API-185; footer count stale (108) | Medium | FIXED |
| 2 | `db/_index.md` missing TBL-27 sla_reports | Medium | FIXED |
| 3 | `GLOSSARY.md` NRF entry said "placeholder for future NF discovery" — now real 3GPP calls | Medium | FIXED |
| 4 | `GLOSSARY.md` SM-DP+ Adapter entry said "future extension point" — HTTPSMDPAdapter is live | Low | FIXED |
| 5 | `ARCHITECTURE.md` header said "108 APIs, 24 tables" — stale | Low | FIXED |
| 6 | `USERTEST.md` missing STORY-063 section | Low | FIXED |

**Total: 6 findings — 6 FIXED, 0 DEFERRED**
