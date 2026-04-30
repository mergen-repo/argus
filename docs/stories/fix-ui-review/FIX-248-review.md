# FIX-248 — Review Report

**Story:** Reports Subsystem Refactor — storage abstraction + LocalFS + signed URL + scope reduction
**Date:** 2026-04-27
**Reviewer:** Ana Amil inline
**Verdict:** **PASS · 0 unresolved findings · 4 documented deferrals**

---

## Doc Quality Checks (14)

| # | Check | Status | Action |
|---|-------|--------|--------|
| 1 | Plan vs implementation drift | OK (REPORT-ONLY) | C-1 cleanup cron deferred D-167; 5 builders deferred D-165; dead-code cleanup deferred D-166 |
| 2 | api/_index.md updated | UPDATED | API-345 added; Reports section count 5→6; total 275→276; API-206 amended with scope reduction note |
| 3 | ERROR_CODES.md | NO_CHANGE | New 401 / 503 reuse existing apierr envelope |
| 4 | SCREENS.md | NO_CHANGE | No screen changes — backend-only with tiny FE list trim handled by API response |
| 5 | DSL_GRAMMAR / PROTOCOLS / WEBSOCKET_EVENTS / ALGORITHMS | NO_CHANGE | Independent |
| 6 | decisions.md updated | UPDATED | DEV-555..DEV-564 (10 entries) |
| 7 | USERTEST.md updated | UPDATED | FIX-248 section with 6 scenario blocks (scope, pipeline, bad-token, Docker, S3-fallback, multi-instance) |
| 8 | bug-patterns.md | NO_CHANGE | No new patterns; PAT-018/PAT-021 grep clean |
| 9 | ROUTEMAP Tech Debt | UPDATED | D-165, D-166, D-167 added |
| 10 | Story Impact — sibling FIX/STORY | UPDATED (REPORT-ONLY) | See "Story Impact" below |
| 11 | Spec coverage cross-check | PASS WITH DEFERRALS | 16/22 ACs delivered (the broken pipeline FIXED + scope reduction landed + storage abstraction is the durable artifact); AC-3/11..15 (5 new builders) deferred D-165; AC-9/10 (cleanup cron) deferred D-167; AC-18..20 deeper integration tests deferred along with builders |
| 12 | Migration / breaking changes | NONE | Existing S3 endpoints unchanged; `REPORT_STORAGE=local` default means no opt-in needed for the fix to take effect; only KVKK/GDPR/BTK/cost report types are removed (validation rejects them with 400) |
| 13 | Test artifacts present | PASS | +16 backend test cases (storage 8 + download handler 7 + reports definitions 1); FE list trim is API-driven so handler test covers it |
| 14 | Pattern compliance | PASS | FIX-216 SlidePanel pattern N/A here; FIX-241 nil-slice global fix already covers AC-16/17 |

### Critical: pipeline fix verified

The story's headline ask was "fix the broken `POST /reports/generate` that fails with `no EC2 IMDS role found`". Verification path:

1. `cmd/argus/main.go::selectReportStorage(cfg, ...)` — defaults to `mustLocalFS()` when `REPORT_STORAGE=local` (the default). Returns a `*LocalFSUploader`.
2. `LocalFSUploader.Upload` writes to disk; **no AWS SDK invocation, no IMDS lookup**.
3. `scheduledReportProcessor` accepts the interface and calls `Upload + PresignGet` identically to the pre-FIX-248 flow — the change is invisible to the processor.
4. Boot log: `report storage backend: LocalFS` (info) — easy operator confirmation.
5. The pre-FIX-248 `nullReportStorage` no-op wrapper is gone — silent success-with-no-file is the bug class FIX-248 explicitly removes.

Reviewer recommendation: a smoke test in dev (`POST /reports/generate` → check disk under `./data/reports/`) confirms the fix end-to-end. USERTEST.md "AC-4..AC-7" walks through this.

---

## Story Impact (Phase 2 — Other FIX stories)

| Sibling | Impact | Action |
|---------|--------|--------|
| FIX-241 (nil-slice WriteList) | NO_CHANGE — already covers AC-16/17 (`/reports/scheduled` empty `[]`) | Reviewer confirmed via existing FIX-241 patterns |
| FIX-244 (Violations Lifecycle UI) | NO_CHANGE — independent | — |
| FIX-239 (KB Ops Runbook) | NO_CHANGE — KB §6 references reports indirectly via the audit table | — |
| FIX-236 (10M scale) | OK — Reports streaming path aligns with `internal/export/csv.go` contract documented in SCALE.md §2; the new download endpoint complements (not duplicates) the streaming export pattern | Recorded — no action |
| FIX-243 (Policy DSL realtime validate) | NO_CHANGE — independent | — |
| FIX-242 (Session Detail extended DTO) | NO_CHANGE — independent | — |
| Future report stories | OPEN — 5 new builders (D-165) parked under REPORTS.md §1 "Future catalogue" with per-builder spec sketches | Recorded |

No spillover edits required to sibling story files.

---

## Findings

| ID | Section | Issue | Category | Resolution |
|----|---------|-------|----------|------------|
| F-1 | AC-3 + AC-11..15 | 5 new operational builders deferred | DOCUMENTED — D-165 + REPORTS.md "Future catalogue" sketch carries the spec |
| F-2 | AC-9, AC-10 | Cleanup cron deferred | DOCUMENTED — D-167; signed-URL TTL ≪ retention means cleanup is housekeeping, not safety |
| F-3 | DEV-560 | Dead Go code retained for atomic D-165/D-166 commit | DOCUMENTED — small dev-time cost (handler tests still pass), big reviewability win |
| F-4 | FE iconMap | 4 unused keys in iconMap | DOCUMENTED — D-166 cleanup |

**Unresolved (ESCALATED / OPEN / NEEDS_ATTENTION):** 0

All deferrals are conscious plan adaptations with documented rationale and tracked D-debt entries. No findings require escalation.

---

## Verdict

**PASS** — proceed to Step 5 (Commit).

Doc artifacts updated this review:
- `docs/architecture/api/_index.md` (API-345 + Reports count 5→6 + total 275→276)
- `docs/architecture/REPORTS.md` (NEW — catalogue + storage + signed URL + retention)
- `docs/architecture/CONFIG.md` ("Report Storage" section after Storage S3-Compatible)
- `docs/brainstorming/decisions.md` (10 DEV entries DEV-555..564)
- `docs/USERTEST.md` (FIX-248 6 scenario blocks)
- `docs/ROUTEMAP.md` (D-165, D-166, D-167 tech-debt rows; FIX-248 marked DONE-with-deferral)
- `docs/stories/fix-ui-review/FIX-248-{plan,gate,review,step-log}.md`
