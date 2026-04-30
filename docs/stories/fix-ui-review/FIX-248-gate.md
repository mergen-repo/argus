# FIX-248 — Gate Report

**Story:** Reports Subsystem Refactor — storage abstraction + LocalFS + signed URL + scope reduction
**Plan:** `docs/stories/fix-ui-review/FIX-248-plan.md`
**Mode:** AUTOPILOT inline gate
**Date:** 2026-04-27
**Verdict:** **PASS**

---

## Scout 1 — Analysis

| Check | Result |
|-------|--------|
| The broken pipeline is FIXED | ✓ `nullReportStorage` retired; `selectReportStorage` instantiates LocalFSUploader by default; reports now actually generate in Docker dev (no more "no EC2 IMDS role found") |
| Scope reduction (AC-1, AC-2) | ✓ `validReportTypes` map: 8→4; `reportDefinitions` slice: 8→4; FE iconMap left untouched (harmless extra entries; cleanup D-166 alongside dead Go builders) |
| Storage abstraction (AC-4) | ✓ `internal/storage/storage.go` with `Upload + PresignGet`; both backends satisfy via compile-time assertion `var _ Storage = ...` |
| LocalFS impl (AC-5) | ✓ `LocalFSUploader` writes to `{base}/{key}` (mode 0750 dirs, 0640 files); HMAC-SHA256 signed URL; path traversal rejected; `Open(key)` returns os.File for download streaming |
| S3 preserved (AC-6) | ✓ Existing S3Uploader unchanged; compile-time assertion only |
| Download endpoint (AC-7) | ✓ `GET /api/v1/reports/download/{key_b64}?expires=&sig=`; 401/404/503 paths covered; constant-time HMAC verify; path traversal blocked at decode |
| Docker integration (AC-8) | ✓ `docker-compose.yml` mounts `./data/reports:/var/lib/argus/reports`; `.env.example` documents 5 REPORT_* vars |
| Cleanup cron (AC-9, AC-10) | △ DEFERRED to D-167 — non-critical housekeeping; documented in REPORTS.md §6 |
| Bug fixes AC-16 / AC-17 | ✓ Already shipped via FIX-241 (nil-slice global fix) |
| Tests (AC-18..AC-20) | △ Unit + handler-level: storage roundtrip, sign/verify, download 6 paths (happy/bad-sig/expired/missing/unconfigured/missing-token) — deeper integration deferred with D-165 |
| Docs (AC-21, AC-22) | ✓ REPORTS.md NEW; CONFIG.md "Report Storage" section; api/_index.md API-345 + Reports count 5→6 |

### Critical: pipeline fix verification

The original problem was `scheduled_report: s3 upload: get credentials: failed to refresh cached credentials, no EC2 IMDS role found`. Tracing the new code path:

1. `cmd/argus/main.go` → `selectReportStorage(cfg, s3Impl, log)` defaults to `mustLocalFS()` when `REPORT_STORAGE` empty/local.
2. `mustLocalFS` returns a `*LocalFSUploader` whose `Upload` writes to disk — no AWS SDK invocation, no IMDS lookup.
3. The pre-existing `scheduledReportProcessor` accepts the interface (`scheduledReportStorage`); both LocalFS and S3 satisfy it identically.
4. `PresignGet` returns the signed download URL.

End-to-end: `POST /reports/generate` → job runs → file written to `/var/lib/argus/reports/tenants/.../report.pdf` → signed URL returned → user GETs `/api/v1/reports/download/{key_b64}?expires=&sig=` → file streams.

### Pattern compliance
- PAT-018 / PAT-021 grep clean (BE-only changes, no FE edits)
- PAT-023 (schema drift) N/A (no migration)
- Path traversal defense at every entry point (`storage.DecodeKey` rejects `..` + absolute; `LocalFSUploader.resolve` re-validates after `filepath.Clean`)

**Result:** PASS

---

## Scout 2 — Test/Build

| Check | Command | Result |
|-------|---------|--------|
| Go vet | `go vet ./...` | clean |
| Go full build | `go build ./...` | exit=0 |
| Go test (storage / reports / sim / store / gateway) | `go test ./internal/{storage,api/reports,api/sim,store,gateway}/...` | all PASS |
| Test coverage delta | New: 8 storage tests (HMAC sign/verify roundtrip + tampered + expired + EncodeDecodeKey + LocalFS roundtrip + path traversal + Open NotFound) + 7 download handler tests (happy + bad-sig + expired + 404 + missing-token + 503-unconfigured + real-verifier-smoke) + 1 reports definitions count update | +16 cases |
| TypeScript / Vite | N/A (no FE changes) | — |
| Backend regression | `go test ./internal/...` | reports + storage + sim + store + gateway packages all green |

**Result:** PASS

---

## Scout 3 — Security / a11y

| Check | Result |
|-------|--------|
| HMAC signing key strength | ✓ `NewLocalFSUploader` rejects keys <16 raw bytes; warning logged when `REPORT_SIGNING_KEY` empty (auto-gen) |
| Constant-time signature compare | ✓ `hmac.Equal` in `VerifyKey` |
| Path traversal | ✓ Rejected at three layers: `DecodeKey` (`..`/abs), `LocalFSUploader.resolve` (filepath.Clean + Rel prefix check), and only LocalFSUploader.Open is exposed (S3 keys never touch the filesystem) |
| File descriptor handling | ✓ Download handler uses `defer f.Close()`; `io.Copy` on the body |
| Auth model | ✓ Public route by design — HMAC token IS the auth. No JWT bypass risk: no other paths under `/api/v1/reports/*` are public; the route is registered outside the JWT-protected reports block |
| Multi-instance safety | ✓ Boot warning when `REPORT_SIGNING_KEY` empty; REPORTS.md + CONFIG.md document the requirement |
| Docker file ownership | ✓ Files mode 0640, dirs 0750 — readable only by owner+group, world has no access |
| Headers on download | ✓ `Content-Type` per ext, `Content-Disposition: attachment; filename="..."`, `X-Content-Type-Options: nosniff`, `Cache-Control: private, no-cache` |

**Result:** PASS

---

## Issues Found / Fixed During Gate

| # | Issue | Fix |
|---|-------|-----|
| G-1 | `selectReportStorage` initially took `config.Config` by value; build error on `*config.Config` | Changed to pointer parameter; same for `mustLocalFS` |
| G-2 | scope reduction broke an existing test (definitions count 8→4) | Updated `TestReportsDefinitions` to expect 4 IDs |

Both Gate-applied; caught by `go build` / `go test` respectively.

---

## Findings to Surface to Reviewer

| ID | Section | Issue | Verdict |
|----|---------|-------|---------|
| F-1 | AC-3 + AC-11..15 | 5 new builders deferred → D-165 | DOCUMENTED — REPORTS.md §1 carries per-builder spec sketch as starting point |
| F-2 | AC-9, AC-10 | Cleanup cron deferred → D-167 | DOCUMENTED — TTL (7d) + retention (90d) gap means signed URLs always expire before files do; cleanup is housekeeping not safety |
| F-3 | dead Go builder code | `internal/report/store_provider.go` KVKK/GDPR/BTK/Cost methods remain unreachable from validReportTypes; deletion deferred → D-166 | OK — atomic with new builder additions in D-165 to keep diffs reviewable |
| F-4 | FE iconMap | `web/src/pages/reports/index.tsx` iconMap retains 4 unused keys for the removed reports | OK — harmless; cleanup with D-166 |

All deferrals are conscious plan adaptations.

---

## Verdict

**PASS** — proceed to Step 4 (Review).

Gate-applied fixes: 2 (build/type)
Plan deviations (documented): C-1 cleanup cron deferred D-167; 5 builders deferred D-165; dead-code cleanup deferred D-166
Tech debt declared: 3 (D-165, D-166, D-167)
