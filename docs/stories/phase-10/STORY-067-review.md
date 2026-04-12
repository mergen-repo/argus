# Review Report: STORY-067 — CI/CD Pipeline, Deployment Strategy & Ops Tooling

Review date: 2026-04-12
Gate status at review start: PASS (Dispatch #2)
has_ui: false (Check 6 UI-sweep SKIPPED)
Tests at review start: 2182 passed in 70 packages

---

## Summary

- **UPDATED: 8 files** across 6 checks (7, 8, 9, 11, 12, 14) — all doc gaps fixed inline, zero deferred
- **ESCALATED: 0**
- **SKIPPED: 1** (Check 6 — UI sweep, has_ui=false)
- **NO-ACTION: 5** (Checks 3, 4, 5, 13 no gaps; Checks 1, 2, 10 are REPORT-ONLY by spec)

All 8 fixes are applied and verified. Story is complete.

---

## Check Results

### Check 1 — AC Completeness (REPORT ONLY)

All 9 ACs verified as implemented:

| AC | Status | Notes |
|----|--------|-------|
| AC-1 Blue-green Docker Compose | DONE | `deploy/docker-compose.blue.yml`, `deploy/docker-compose.green.yml` — distinct port ranges |
| AC-2 Bluegreen-flip + rollback scripts | DONE | `deploy/scripts/bluegreen-flip.sh`, `deploy/scripts/rollback.sh`, `deploy/scripts/smoke-test.sh` |
| AC-3 Digest pinning | DONE | All FROM statements pinned `@sha256:...`; `infra/scripts/update-digests.sh` |
| AC-4 Rollback audit | DONE | `rollback.sh` POSTs to `/api/v1/audit/system-events`; hard-fails on non-2xx |
| AC-5 GitHub Actions CI (5 stages) | DONE | `.github/workflows/ci.yml` — lint/test/security-scan/build/deploy |
| AC-6 argusctl CLI | DONE | `cmd/argusctl/` — cobra binary, ARGUSCTL_ Viper prefix, `make build-ctl` |
| AC-7 GET /api/v1/status | DONE | Public aggregate + auth-gated `/details`; `internal/api/system/status_handler.go` |
| AC-8 Deploy audit + git tag + build_info | DONE | `bluegreen-flip.sh` POSTs audit; `deploy-tag.sh` creates git tag; `argus_build_info` gauge |
| AC-9 Runbooks (10 docs) | DONE | `docs/runbook/` — 10 files (+ dr-restore.md scope addition) |

Scope additions beyond plan accepted by Gate:
- `DELETE /api/v1/users/:id?gdpr=1` → `UserStore.DeletePII`, users.state=purged
- Migration `20260412000010_users_state_purged` — adds `purged` to `users.state` CHECK
- `POST /api/v1/audit/system-events` — super_admin, TenantID=uuid.Nil, hash chain via ProcessEntry
- Tenant suspend/resume via existing `PATCH /api/v1/tenants/:id` (state field)
- `/api/v1/status` split into two routes (public aggregate + auth-gated detail)

### Check 2 — Gate Findings Resolution (REPORT ONLY)

| Finding | Severity | Status |
|---------|---------|--------|
| F-1: Audit endpoint missing, scripts silent-fail | HIGH | FIXED (Dispatch #2) |
| F-2: E2E evidence file missing | MEDIUM | FIXED (Dispatch #2) — 316-line doc |
| F-3: CI `on: push:` lacks branch filter | LOW | ACCEPTED deviation |
| F-4: /api/v1/status split into two routes | LOW | ACCEPTED deviation |

### Check 3 — Zero-Deferral Scan

`grep -rn "TODO\|FIXME\|HACK\|STUB" internal/api/audit internal/api/system deploy/scripts cmd/argusctl` — **0 hits**

Production code is clean. **PASS**

### Check 4 — Test Regression

`go test ./... -race -short` → **2182 passed in 70 packages**

Baseline was 2175 pre-escalation; post-escalation is 2182 (+7 new tests for EmitSystemEvent handler + router auth). No regression. **PASS**

### Check 5 — FUTURE.md Review

No new future opportunities emerge from STORY-067 that are not already captured (CI/CD tooling is implementation, not product capability). Existing future items unchanged. **NO-ACTION**

### Check 6 — UI Sweep

SKIPPED — has_ui: false.

### Check 7 — Makefile & Config Verification

Makefile verified: `lint`, `security-scan`, `deploy-staging`, `deploy-prod`, `rollback`, `ops-status`, `build-ctl` targets all present and wired to correct scripts.

CONFIG.md was missing: `ARGUS_API_URL`, `ARGUS_API_TOKEN` (deploy scripts), and all `ARGUSCTL_*` vars.

**FIX APPLIED:** Added `## CI/CD & Ops Tooling (STORY-067)` section to `docs/architecture/CONFIG.md` with deploy-script vars and argusctl var table. **UPDATED**

### Check 8 — Decisions Log

`docs/brainstorming/decisions.md` had no STORY-067 entries. Last entry was DEV-185 (STORY-066).

**FIX APPLIED:** Added DEV-186 through DEV-192:
- DEV-186: Blue-green Docker Compose port strategy (vs Kubernetes)
- DEV-187: Deploy snapshot JSON format and storage location
- DEV-188: argusctl Viper ARGUSCTL_ prefix and auth hierarchy
- DEV-189: POST /api/v1/audit/system-events uses TenantID=uuid.Nil; hard-fail semantics
- DEV-190: pgbouncer:1.22.1 tag unavailable — digest-pinned to latest
- DEV-191: recent_error_5m hardcoded to 0 (DEFERRED — plan-permitted)
- DEV-192: Tenant suspend/resume via PATCH; /api/v1/status split accepted

**UPDATED**

### Check 9 — API Index

`docs/architecture/api/_index.md` was missing all 4 new endpoints.

**FIX APPLIED:**
- Added `API-192 GET /api/v1/status` (System Health section, public)
- Added `API-193 GET /api/v1/status/details` (System Health section, super_admin)
- Added `API-194 POST /api/v1/audit/system-events` (Audit section, super_admin)
- Added `API-195 DELETE /api/v1/users/:id?gdpr=1` (Users section, super_admin)
- Updated section headers: System Health 8→10, Audit 3→4
- Updated footer: 114→118 REST endpoints

**UPDATED**

### Check 10 — DB Schema Docs (REPORT ONLY)

TBL-02 users.state column in `docs/architecture/db/platform.md` listed only `active, disabled, invited`. Migration `20260412000010` adds `purged`.

**FIX APPLIED (Check 11 scope):** Updated TBL-02 state column to include `purged`. **UPDATED**

### Check 11 — DB Schema Doc Fix

See Check 10 above. Fix confirmed at `docs/architecture/db/platform.md` line 41. **UPDATED**

### Check 12 — GLOSSARY.md

Three new terms were missing: Blue-Green Deployment, argusctl, Digest Pinning.

**FIX APPLIED:** Added all three terms to the `## Reliability & DR Terms` section. **UPDATED**

### Check 13 — Tech Debt

ROUTEMAP.md Tech Debt section: D-001 through D-005. None target STORY-067. No unresolved tech debt items from this story. **NO-ACTION**

### Check 14 — ARCHITECTURE.md & ROUTEMAP.md

Multiple gaps found:

**ARCHITECTURE.md:**
- Header scale: `114 APIs` → `118 APIs`
- Project tree: `cmd/` missing `argusctl/` entry
- Project tree: `deploy/` missing blue/green compose files and scripts/
- No CI/CD Pipeline & Ops Tooling section
- Reference ID Registry: API-NNN 115/API-001..191 → 119/API-001..195

**ROUTEMAP.md:**
- STORY-067 row: `[~] IN PROGRESS | Review` → `[x] DONE | 2026-04-12`
- Phase 10 counter: 10/22 → 11/22
- Global counter: `Phase 10: 10/22` → `Phase 10: 11/22`
- Current story: STORY-067 → STORY-068
- Changelog: no STORY-067 entry

**USERTEST.md:**
- No STORY-067 section

**FIX APPLIED (all):**
- ARCHITECTURE.md: header updated, cmd/argusctl/ added to tree, deploy/scripts/ and blue/green compose added to tree, CI/CD & Ops Tooling section added, Reference ID Registry updated
- ROUTEMAP.md: STORY-067 marked DONE, counter 11/22, current story STORY-068, changelog entry added
- USERTEST.md: STORY-067 backend verification section added (15 scenarios)

**UPDATED (x3)**

---

## Issues Table

| # | Check | Severity | File | Finding | Resolution |
|---|-------|---------|------|---------|------------|
| 1 | 7 | MEDIUM | docs/architecture/CONFIG.md | ARGUS_API_URL, ARGUS_API_TOKEN, ARGUSCTL_* vars not documented | FIXED — CI/CD & Ops Tooling section added |
| 2 | 8 | MEDIUM | docs/brainstorming/decisions.md | No STORY-067 DEV entries (7 design decisions undocumented) | FIXED — DEV-186..192 added |
| 3 | 9 | MEDIUM | docs/architecture/api/_index.md | 4 new endpoints (API-192..195) missing; footer count wrong (114 vs 118) | FIXED — all 4 endpoints added, footer updated |
| 4 | 11 | LOW | docs/architecture/db/platform.md | TBL-02 users.state missing `purged` (migration 20260412000010) | FIXED — state column updated |
| 5 | 12 | LOW | docs/GLOSSARY.md | Blue-Green Deployment, argusctl, Digest Pinning terms missing | FIXED — all 3 terms added |
| 6 | 14 | HIGH | docs/ARCHITECTURE.md | cmd/argusctl/ missing from tree; deploy/ scripts missing; API count 114→118; no CI/CD section; Registry count stale | FIXED — all gaps addressed |
| 7 | 14 | MEDIUM | docs/ROUTEMAP.md | STORY-067 still IN PROGRESS; counter 10/22; no changelog entry | FIXED — DONE, counter 11/22, changelog added |
| 8 | 14 | MEDIUM | docs/USERTEST.md | No STORY-067 verification section | FIXED — 15 backend scenarios added |

---

## Files Modified

| File | Change |
|------|--------|
| `docs/architecture/CONFIG.md` | Added CI/CD & Ops Tooling section (ARGUS_API_URL, ARGUS_API_TOKEN, ARGUSCTL_*) |
| `docs/brainstorming/decisions.md` | Added DEV-186..192 (7 STORY-067 design decisions) |
| `docs/architecture/api/_index.md` | Added API-192..195; updated Audit section 3→4, System Health 8→10; footer 114→118 |
| `docs/architecture/db/platform.md` | TBL-02 users.state: added `purged` state + migration reference |
| `docs/GLOSSARY.md` | Added Blue-Green Deployment, argusctl, Digest Pinning terms |
| `docs/ARCHITECTURE.md` | Header 114→118 APIs; cmd/argusctl/ tree entry; deploy scripts tree; CI/CD section; Registry 115→119 |
| `docs/ROUTEMAP.md` | STORY-067 DONE 2026-04-12; counter 11/22; current story STORY-068; changelog entry |
| `docs/USERTEST.md` | STORY-067 section added (15 backend verification scenarios) |

---

**Review status: COMPLETE — 8 UPDATED, 0 ESCALATED, 0 DEFERRED**
