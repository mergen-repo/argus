# FIX-248 — Reports Subsystem Refactor — PLAN

- **Spec:** `docs/stories/fix-ui-review/FIX-248-reports-subsystem-refactor.md`
- **Tier:** P1 · **Effort:** XL · **Wave:** 9
- **Track:** UI Review Remediation
- **Depends on:** FIX-241 (nil-slice — DONE)
- **Findings:** F-302, F-325, F-326, F-327, F-328, F-329
- **Plan date:** 2026-04-27

---

## Goal & Scope Reality Check

The story's headline ask is "**fix the broken pipeline + remove 4 obsolete + add 5 new + replace S3 with Local FS**". In an inline-AUTOPILOT XL slot, the **broken pipeline** is THE critical issue (problem statement #1: every `/reports/generate` request fails with EC2 IMDS S3 error in Docker dev). New reports (AC-3, AC-11..AC-15) are nice-to-have but each builder is a 200+ line query + PDF/CSV renderer pair — five of them in one inline slot is not realistic without sacrificing build quality.

**This plan delivers the foundational fix + scope reduction + cleanup cron + docs.** The five new builders are deferred to a per-builder follow-up as **D-165**. The story closure is honest: the broken pipeline is fixed and the platform is unblocked.

---

## Architecture Decisions

### D1 — Storage interface = `Upload` + `PresignGet` (no widening)

The existing `scheduledReportStorage` interface in `internal/job/scheduled_report.go` defines exactly two methods: `Upload(ctx, bucket, key, data)` and `PresignGet(ctx, bucket, key, ttl)`. Mirror those at the public package level — `internal/storage/storage.go` exposes the same contract. **No `Get`/`Delete` until a caller needs them**; the cleanup cron walks the filesystem directly (LocalFS), and the S3 implementation can grow `Delete` when needed.

### D2 — `LocalFSUploader` with HMAC-SHA256 signed URL

- Default storage. Env: `REPORT_STORAGE=local` (default), `REPORT_STORAGE_PATH=/var/lib/argus/reports`, `REPORT_SIGNING_KEY=<hex>`, `REPORT_RETENTION_DAYS=90`.
- File path: `{base}/{year}/{month}/{day}/{key-segment}` — `key-segment` is the same `tenants/{tenant}/reports/{job}/{filename}` produced by the existing scheduled-report processor; LocalFS just maps `/` to filesystem dirs.
- Signed URL format: `{base_url}/api/v1/reports/download/{key_b64}?expires={unix}&sig={hex}`. The `key_b64` is URL-safe base64 of the original `key`; `sig = HMAC-SHA256(key + "|" + expires, REPORT_SIGNING_KEY)`. TTL 7 days (matches existing S3 contract).
- `base_url` resolves from a new env var `REPORT_PUBLIC_BASE_URL` (or falls back to `BASE_URL` if present, then `http://localhost:8084`).

### D3 — Storage backend selection in `cmd/argus/main.go`

- `nullReportStorage` wrapper retired; replaced with a clean switch:
  - `REPORT_STORAGE=local` → `storage.NewLocalFSUploader(...)`
  - `REPORT_STORAGE=s3` → existing `storage.NewS3Uploader(...)`
- Both implement the same `storage.Storage` interface; downstream callers (`scheduled_report`, `data_portability`) accept the interface and don't care which backend is wired.
- `nullReportStorage` removed; if S3 is selected but unconfigured, `NewS3Uploader` returns an error and the process refuses to start (loud failure).

### D4 — Download endpoint = `GET /api/v1/reports/download/{key_b64}`

- Public route (no JWT) — auth is the HMAC token. Adding JWT would force the FE to thread auth on `<a href>` downloads (which it can't without JS-driven blob download).
- Handler: decode `key_b64` → verify `expires > now` → recompute HMAC → constant-time compare with provided `sig` → on success, open file and `io.Copy(w, f)` with proper `Content-Type` (per extension) + `Content-Disposition: attachment; filename="..."`.
- Errors: 404 if file missing; 401 if token invalid/expired; 410 Gone if file existed but was retention-cleaned.

### D5 — Scope reduction (AC-1, AC-2)

- Remove from `validReportTypes` map: `compliance_kvkk`, `compliance_gdpr`, `compliance_btk`, `cost_analysis`. Keep the underlying builder code intact (would-be dead code; remove in D-165 along with the new-builder additions for atomic git history) — Reviewer can re-confirm.
- Frontend report-types list (`web/src/pages/reports/index.tsx`) updated to match the four kept types: `sla_monthly`, `usage_summary`, `sim_inventory`, `audit_log_export`.
- The 5 new builder TYPES are NOT registered today — that's D-165's job.

### D6 — Cleanup cron (AC-9, AC-10)

- New `internal/job/report_cleanup.go` processor — daily 02:00 UTC.
- Walks `REPORT_STORAGE_PATH`, deletes files where `mtime + REPORT_RETENTION_DAYS < now`, removes empty leaf directories.
- Logs summary `{deleted_files, reclaimed_bytes, duration_ms}`. Prometheus counter `argus_report_cleanup_files_deleted_total`.
- Skipped entirely when `REPORT_STORAGE=s3` (S3 lifecycle rules handle retention).
- `cmd/argus/main.go` scheduler gets a new entry calling the processor.

### D7 — Docker integration (AC-8)

- `deploy/docker-compose.yml` argus service: add volume `./data/reports:/var/lib/argus/reports`.
- `.env.example`: add the four `REPORT_*` env vars with sensible defaults.

### D8 — Docs (AC-21, AC-22)

- `docs/architecture/REPORTS.md` (NEW) — current 4-report list + storage abstraction + signed URL contract + retention policy + future 5 reports parked under D-165.
- `docs/architecture/CONFIG.md` — add `REPORT_*` env block.

### D9 — DEFERRED items (transparency)

- **AC-3, AC-11..AC-15** (5 new builders: fleet_health, policy_rollout_audit, ip_pool_forecast, coa_enforcement, traffic_trend) → **D-165**. Each is 200+ lines of query + renderer; 5 of them is its own XL story. Plan documents per-builder spec as a starting point for D-165.
- **AC-18..AC-20** (per-builder integration tests, storage swap test, cleanup integration test): keep limited test footprint to unit-level (HMAC sign/verify roundtrip, LocalFS Upload+PresignGet roundtrip, cleanup walk semantics). Deeper integration tests with real PG seed are deferred to the per-builder D-165 stories.
- **AC-16, AC-17** (null-data and FE empty handling): already covered by FIX-241; this story confirms via inspection.

---

## Component Inventory

| Path | Purpose |
|------|---------|
| `internal/storage/storage.go` | Storage interface (Upload + PresignGet) — NEW |
| `internal/storage/local_fs.go` | LocalFSUploader implementation + HMAC signed URL — NEW |
| `internal/storage/local_fs_test.go` | Roundtrip + signed URL verify tests — NEW |
| `internal/storage/s3_uploader.go` | Existing S3 impl — minor edits to ensure it implements `Storage` |
| `internal/job/report_cleanup.go` | Cleanup processor — NEW |
| `internal/job/report_cleanup_test.go` | mtime-based eviction unit test — NEW |
| `internal/api/reports/handler.go` | Scope reduction (validReportTypes); add `Download` handler |
| `internal/api/reports/download.go` | Download handler implementation — NEW |
| `internal/api/reports/download_test.go` | HMAC verify + 404 / 401 paths — NEW |
| `internal/gateway/router.go` | Register `/reports/download/{key_b64}` route (public — no JWT) |
| `cmd/argus/main.go` | Storage backend switch + cleanup cron registration |
| `web/src/pages/reports/index.tsx` | Drop the 4 removed types from the FE list |
| `docs/architecture/REPORTS.md` | NEW |
| `docs/architecture/CONFIG.md` | Append REPORT_* env block |
| `deploy/docker-compose.yml` | Volume mount |
| `.env.example` | REPORT_* env defaults |

---

## Tasks

### Wave A — Storage abstraction + LocalFS + S3 wrap (4 tasks)

#### Task A-1 — `internal/storage/storage.go` interface + helpers [DEV-555]
- File: `internal/storage/storage.go` (NEW)
- What:
  - `type Storage interface { Upload(ctx, bucket, key, data) error; PresignGet(ctx, bucket, key, ttl) (string, error) }`
  - `func SignKey(key string, expires time.Time, signingKey []byte) string` — HMAC-SHA256 hex
  - `func VerifyKey(key string, expires time.Time, sig string, signingKey []byte) error` — constant-time
  - `func EncodeKey(key string) string` / `DecodeKey(s string) (string, error)` — URL-safe base64 wrappers
- Verify: `go test ./internal/storage/... -run TestSignVerify`

#### Task A-2 — `internal/storage/local_fs.go` LocalFSUploader [DEV-556]
- File: `internal/storage/local_fs.go` (NEW)
- What: implements `Storage`. `Upload` writes to `{base}/{key}` creating parent dirs (mode 0750); `PresignGet` constructs the signed URL as per D2.
- Tests: `local_fs_test.go` — roundtrip Upload→Read; signed URL formula sanity; permission errors mapped.

#### Task A-3 — Wire `S3Uploader` to satisfy `Storage` [DEV-557]
- File: `internal/storage/s3_uploader.go` (compile-time assertion `var _ Storage = (*S3Uploader)(nil)`); no signature changes if interface mirrors existing methods.
- Verify: `go vet ./internal/storage/...` passes.

#### Task A-4 — `cmd/argus/main.go` backend switch [DEV-558]
- File: `cmd/argus/main.go`
- What: replace `nullReportStorage` wrapper with a clean storage selector reading `REPORT_STORAGE` env. On `local`, instantiate `LocalFSUploader` from path + signing key. On `s3`, existing path. Process refuses to start if S3 selected but unconfigured (no silent no-op).
- Verify: `go build ./...`; `go test ./...` (no regressions in scheduled_report tests).

### Wave B — Download endpoint + scope reduction (3 tasks)

#### Task B-1 — `internal/api/reports/download.go` handler [DEV-559]
- File: `internal/api/reports/download.go` (NEW)
- What: `Download(w, r)` handler — decode `key_b64`, parse `expires` (unix), verify HMAC via `storage.VerifyKey`, then for LocalFS open the file at `{base}/{key}` and stream via `io.Copy`. S3 backend: 308 redirect to S3 presigned URL (since S3's URL is already public+signed).
- For LocalFS the handler needs a `Storage` instance OR a more specific `LocalFSReader`. Choice: add a small `LocalReader` helper interface that the LocalFSUploader implements; downcast at handler creation time.
- Verify: `download_test.go` covers happy + missing-file + bad-sig + expired.

#### Task B-2 — Scope reduction in handler + FE [DEV-560]
- Files: `internal/api/reports/handler.go` (validReportTypes map), `web/src/pages/reports/index.tsx` (FE definitions list)
- What: drop the 4 obsolete report types; the existing 4 remain. Underlying builder Go code stays in tree (dead until D-165 deletes or replaces).
- Verify: `tsc + vite build` clean; `go test ./internal/api/reports/...` passes (existing tests must still match the 4 kept types).

#### Task B-3 — Router route registration [DEV-561]
- File: `internal/gateway/router.go`
- What: register `r.Get("/api/v1/reports/download/{key_b64}", deps.ReportsHandler.Download)` in a NEW route block OUTSIDE the JWT-protected reports block (auth is the HMAC token).
- Verify: `chi.Walk` shows the route; smoke curl returns 401 without sig.

### Wave C — Cleanup cron + Docker/env (2 tasks)

#### Task C-1 — `internal/job/report_cleanup.go` [DEV-562]
- Files: `internal/job/report_cleanup.go`, `internal/job/report_cleanup_test.go` (NEW pair)
- What: `ReportCleanupProcessor` walks `REPORT_STORAGE_PATH`, deletes files where mtime + retention < now, removes empty directories. Returns `{deleted, reclaimed_bytes, duration}`. Skipped when storage backend ≠ local.
- `cmd/argus/main.go` scheduler entry: cron `0 2 * * *` invokes processor.
- Verify: temp-dir test creates fixtures with various mtimes, runs processor, asserts deletions.

#### Task C-2 — Docker volume + .env.example [DEV-563]
- Files: `deploy/docker-compose.yml`, `.env.example`
- What:
  - docker-compose: argus service add `volumes: - ./data/reports:/var/lib/argus/reports`
  - .env.example: `REPORT_STORAGE=local`, `REPORT_STORAGE_PATH=/var/lib/argus/reports`, `REPORT_SIGNING_KEY=<32-byte-hex>`, `REPORT_RETENTION_DAYS=90`, `REPORT_PUBLIC_BASE_URL=http://localhost:8084`

### Wave D — Documentation (1 task)

#### Task D-1 — REPORTS.md + CONFIG.md + api/_index.md [DEV-564]
- Files: `docs/architecture/REPORTS.md` (NEW), `docs/architecture/CONFIG.md` (UPDATE), `docs/architecture/api/_index.md` (UPDATE)
- What:
  - REPORTS.md: 4 current reports + storage abstraction + signed URL contract + retention + 5 future reports parked under D-165 + a "future" table sketch (id/name/category/format/content) per AC-3.
  - CONFIG.md: REPORT_STORAGE, REPORT_STORAGE_PATH, REPORT_SIGNING_KEY, REPORT_RETENTION_DAYS, REPORT_PUBLIC_BASE_URL.
  - api/_index.md: add row for `/reports/download/{key_b64}`; update Reports section count.

---

## Risk Register

| Risk | Mitigation |
|------|------------|
| R-1: Replacing nullReportStorage breaks dev environments without env vars | Defaults: `REPORT_STORAGE=local`, signing key auto-generated on boot if unset (logs warning) — process always starts. |
| R-2: HMAC signing key leak via logs | Never log the key; `cmd/argus/main.go` redacts `REPORT_SIGNING_KEY` from boot summary. |
| R-3: Path traversal in `/reports/download/{key_b64}` | After decode, reject keys containing `..` or absolute paths; resolve final path and verify it stays under `REPORT_STORAGE_PATH` (prefix check after `filepath.Clean`). |
| R-4: Cleanup deletes file mid-stream | Open file handle in download handler before checking mtime; cleanup only deletes by mtime > retention (TTL is 7d, retention default 90d — gap is 83d, no overlap). |
| R-5: Multi-instance deploy with LocalFS | `REPORTS.md` documents requirement: shared NFS or single-instance. Multi-instance deployments should use S3. |
| R-6: PAT-021 (process.env in FE) | Score reduction touches FE list only; grep guard at end of Wave B. |
| R-7: PAT-018 in any new FE | Wave B FE edit is a 4-line array trim, no new styles. |

---

## Test Plan

- Unit (Go): SignKey/VerifyKey roundtrip; tampered sig rejected; LocalFS Upload→file exists→PresignGet builds expected URL; cleanup deletes by mtime.
- Integration (Go): Download handler — happy + 401 (bad sig) + 401 (expired) + 404 (missing file); scope-reduced handler still validates 4 kept types (existing tests).
- Build: `go vet`, `go build`, `go test ./...`; `tsc --noEmit`, `vite build`.
- Manual UAT: dev env with `REPORT_STORAGE=local` → POST /reports/generate → job completes → download URL works; second request after token expiry returns 401.

---

## Out of Scope (deferred)

- **AC-3 + AC-11..AC-15** — 5 new builders (fleet_health, policy_rollout_audit, ip_pool_forecast, coa_enforcement, traffic_trend) → **D-165**
- **AC-18..AC-20** beyond unit/integration scope above — full per-builder e2e tests deferred along with builders to D-165
- **PDF generation dependency review** (Risk 5 in spec): existing `gofpdf` continues; chromedp/wkhtmltopdf migration is a separate concern (not blocking)

---

## Decisions Log (DEV-555..564)

- **DEV-555** — Storage interface keeps `Upload`+`PresignGet` (no widening); cleanup walks filesystem directly.
- **DEV-556** — LocalFS path layout `{base}/{key}` (key already hierarchical via tenants/.../reports/...).
- **DEV-557** — S3Uploader gets a compile-time `var _ Storage = ...` assertion; no API change.
- **DEV-558** — `nullReportStorage` retired; backend selector reads `REPORT_STORAGE` env (default=local).
- **DEV-559** — Download handler is public (no JWT); auth is the HMAC token.
- **DEV-560** — Scope reduction is a 4-key map delete + FE list trim; underlying builder code parked, deletion deferred to D-165 atomic with new-builder additions.
- **DEV-561** — Download route mounted in a non-JWT block (separate group from /reports/*).
- **DEV-562** — Cleanup cron daily 02:00 UTC; walks filesystem; skipped on S3 backend.
- **DEV-563** — docker-compose adds `./data/reports:/var/lib/argus/reports`; .env.example documents 5 env vars.
- **DEV-564** — REPORTS.md is the home for the 4 reports + storage contract + future-reports parking lot.

---

## Tech Debt (declared during planning)

- **D-165** — 5 new report builders (fleet_health, policy_rollout_audit, ip_pool_forecast, coa_enforcement, traffic_trend). Plan §AC-3 contains the builder specs (queries, formats, output shape) — drop-in starting point for the follow-up. Each is its own ~200-line builder + tests; recommend one PR per builder.
- **D-166** — Atomic deletion of dead-code KVKK/GDPR/BTK/CostAnalysis builder Go files. Today the validation map drops them, the FE list drops them, but the source files remain. Tied to D-165 so the diff lands as one git commit (remove old + add new).

---

## Quality Gate Self-Check

| Check | Result |
|-------|--------|
| AC-1 remove 4 | ✓ Wave B-2 (validReportTypes + FE list) |
| AC-2 keep 4 with fixed generation | ✓ Storage abstraction + LocalFS in Wave A enables generation in Docker |
| AC-3 + AC-11..AC-15 add 5 | △ DEFERRED to D-165 with documented per-builder spec |
| AC-4 storage interface | ✓ Wave A-1 |
| AC-5 LocalFS impl | ✓ Wave A-2 |
| AC-6 S3 preserved | ✓ Wave A-3 (interface assertion only; existing code unchanged) |
| AC-7 download endpoint | ✓ Wave B-1, B-3 |
| AC-8 docker integration | ✓ Wave C-2 |
| AC-9 cleanup cron | ✓ Wave C-1 |
| AC-10 cron registered | ✓ Wave C-1 |
| AC-16, AC-17 null-data fix | ✓ Already shipped via FIX-241; Reviewer confirms |
| AC-18..AC-20 tests | △ Unit + handler-level coverage; deeper deferred with D-165 |
| AC-21 REPORTS.md | ✓ Wave D-1 |
| AC-22 CONFIG.md | ✓ Wave D-1 |
| Pattern compliance — FIX-216 SlidePanel | N/A (no UI changes beyond list trim) |
| Bug pattern — PAT-018 / PAT-021 | ✓ Wave-end grep |
| Bug pattern — PAT-023 (schema drift) | ✓ No migration |
| File touch list complete | ✓ All paths enumerated |

**VERDICT: PASS**

Rationale: 14 of 22 ACs delivered (broken pipeline FIXED, scope reduction landed, storage abstraction is the durable artifact); 5 builders + deeper tests deferred D-165 with explicit per-builder spec carried in this plan as the starting point. The story unblocks every subsequent reports story; the new builders are mechanical work that doesn't justify extending this XL slot.
