# Post-Story Review: FIX-246 — Quotas + Resources merge → Unified "Tenant Usage" Dashboard

**Reviewer:** Autopilot Reviewer Agent
**Date:** 2026-04-27
**Story:** FIX-246 (Wave 10 P2 — UI Review Remediation)
**Gate:** PASS (FIX-246-gate.md)
**Verdict: PASS**

---

## 1. Story Summary (Report-Only)

FIX-246 merged two legacy admin pages (`/admin/quotas` + `/admin/resources`) into a single unified "Tenant Usage" dashboard at `/admin/tenant-usage`. Delivered:

- New page `tenant-usage.tsx` with card/table toggle, URL-backed sort/search/filter toolbar, 30s auto-refresh, and SlidePanel drill-down
- 4-tier quota bar (success/warning/danger/critical with pulse ring at ≥80% and ≥95%)
- Background job `quota_breach_checker` inserting `type="quota.breach"` alerts at 80%/95% per-tenant per-metric
- `EstimateAPIRPS` new TenantStore method for live API RPS estimation from audit_logs
- DB migration: plan enum CHECK constraint + realistic M2M-scale defaults via GREATEST
- `formatBytes` helper in `web/src/lib/format.ts` (reused everywhere)
- Gate CRITICAL F-A1 fixed: Source was `"quota_breach_checker"`, corrected to `"system"` per `chk_alerts_source` CHECK constraint
- 844 Go tests PASS / 0 FAIL; tsc 0 errors; Vite build 2.66s

---

## 2. Check Results

### Check 1 — Gate Report Verification (Report-Only)
- Gate verdict: **PASS**
- 844 tests pass (job + api/admin + store packages)
- 1 CRITICAL fixed (F-A1: Source="system"), 2 HIGH fixed, 4 MEDIUM fixed (+1 PARTIAL→PASS), 7 LOW fixed
- Token enforcement: 0 hex, 0 arbitrary px, 0 raw HTML elements after fixes
- AC-11 remains PARTIAL (D-170 sparkline deferred), AC-12 PARTIAL→PASS via drill-down link

### Check 2 — ARCHITECTURE.md Route Table
- **Finding:** `/admin/tenant-usage` route was missing from the frontend route table. Old routes `/admin/quotas` and `/admin/resources` had no entries.
- **Action:** Added `/admin/tenant-usage` + redirect entries for the two deprecated routes.
- **Status:** FIXED

### Check 3 — SCREENS.md
- **Finding:** SCR-140 still pointed to `/admin/resources` (STORY-073); SCR-141 still pointed to `/admin/quotas` (STORY-073). Both pages were deleted by FIX-246.
- **Action:** Updated SCR-140 to the new Tenant Usage dashboard (`/admin/tenant-usage`, FIX-246). Marked SCR-141 as MERGED INTO SCR-140.
- **Status:** FIXED

### Check 4 — USERTEST.md
- **Finding:** No FIX-246 section existed.
- **Action:** Added `## FIX-246` section with 7 manual test scenarios covering: card/table toggle, 80%/95% color ring, alert integration, formatBytes display, 30s poll, sort/filter/search toolbar, and breach drill-down link.
- **Status:** FIXED

### Check 5 — decisions.md DEV Entries
- **Finding:** No DEV entries for FIX-246 key decisions (Source='system', BreachSection drill-down, int64 split, plan enum).
- **Action:** Added DEV-566..DEV-570 covering these 5 decisions.
- **Status:** FIXED

### Check 6 — GLOSSARY.md
- **Finding:** Terms "Tenant Usage Dashboard", "Quota Bar", "Quota Breach Checker", "formatBytes" were absent.
- **Action:** Added all 4 terms to the glossary.
- **Status:** FIXED

### Check 7 — ROUTEMAP.md Closure
- **Finding:** FIX-246 status was `[~] IN PROGRESS · Review`.
- **Action:** Updated to `[x] DONE 2026-04-27`.
- **Status:** FIXED

### Check 8 — ROUTEMAP.md Tech Debt (D-170, D-171)
- **Finding:** D-170 (7-day trend sparkline endpoint) and D-171 (recent_breaches type field deep payload) were named in the gate deferred list but absent from the tech debt table.
- **Action:** Added D-170 and D-171 rows to the tech debt table.
- **Status:** FIXED

### Check 9 — ROUTEMAP.md D-153 + D-156 Re-routing
- **Finding:** D-153 (`tenant.quota_breach` NATS catalog entry) and D-156 (`digest.Worker.checkQuotaBreachCount` NO-OP) were both targeted at FIX-246. FIX-246 took the `alerts` approach (not NATS catalog), so neither was addressed.
- **Determination:** FIX-246 implemented quota breach detection via direct DB inserts into the `alerts` table with `source="system"` and `type="quota.breach"` — intentionally NOT via NATS events. D-153's NATS catalog entry and D-156's digest flip-to-live are separate concerns. Re-routing both to future stories is correct; marking them UNADDRESSED-BY-FIX-246 with forward routing.
- **Action:** Updated D-153 and D-156 target columns with clarifying note that FIX-246 chose the alerts-table approach; re-routed to future quota/notification story.
- **Status:** FIXED

### Check 10 — ui-review-2026-04-19.md Closure (Report-Only)
- Findings closed by FIX-246: **F-271, F-272, F-273, F-274, F-314, F-315, F-316, F-317**
- All 8 findings annotated with `CLOSED FIX-246 2026-04-27`.

### Check 11 — Bug Patterns (bug-patterns.md)
- **Finding:** Gate CRITICAL F-A1 revealed a new bug class: unit test fakes for AlertStore did not replicate PostgreSQL CHECK constraint validation. The `Source="quota_breach_checker"` violation was invisible until a DB integration test or a live DB run. No existing PAT-NNN captured this.
- **Action:** Added PAT-024: "Fake store silences PostgreSQL CHECK constraint violations — unit test fakes must either replicate DB-level CHECK logic or be supplemented by a DB integration test for every INSERT that touches a CHECK-constrained column."
- **Status:** FIXED

### Check 12 — D-153 + D-156 Scope Accuracy
- Addressed under Check 9 above. Both defer entries clarified and re-routed.

### Check 13 — Tech Debt Pickup
- D-153: OPEN, re-routed. No inline pickup possible (requires NATS catalog plumbing separate from alerts pipeline).
- D-156: OPEN, re-routed. `checkQuotaBreachCount` in `digest/worker.go` cannot be flipped to live until quota breach events flow through NATS (D-153 prerequisite). Acceptable deferral with explicit re-route.
- D-168 (compliance.tsx legacy hook migration): OPEN, correctly deferred per gate F-A11. No pickup needed here.
- D-170 + D-171: Now officially logged. D-170 requires a new `GET /api/v1/admin/tenants/{id}/usage/trend` endpoint (7-day). D-171 requires enriching `recent_breaches` field with full event payload. Both are SlidePanel polish, non-blocking for current usage.

### Check 14 — Mock Sweep
- Gate grep: `grep -rn 'Source:' internal/job/quota_breach_checker.go` → only `"system"` remains. PASS.
- Unit test fakes: `quotaBreachTenantStore` interface in `quota_breach_checker_test.go` + `fakeAlertStore` now have F-A2 (EstimateAPIRPS) wired. PAT-024 logged.
- No new raw HTML elements, no `text-[arbitrary]` values, no hex colors in delivered files.

---

## 3. Summary of Actions Taken

| Doc | Change |
|-----|--------|
| `docs/ARCHITECTURE.md` | Added `/admin/tenant-usage` route + 2 redirect entries |
| `docs/SCREENS.md` | Updated SCR-140 to new page; SCR-141 marked MERGED |
| `docs/USERTEST.md` | Added FIX-246 section — 7 manual test scenarios |
| `docs/brainstorming/decisions.md` | Added DEV-566..DEV-570 (5 key decisions) |
| `docs/GLOSSARY.md` | Added 4 new terms |
| `docs/ROUTEMAP.md` | FIX-246 DONE; D-170 + D-171 added; D-153 + D-156 re-routed |
| `docs/reviews/ui-review-2026-04-19.md` | F-271..274, F-314..317 annotated CLOSED |
| `docs/brainstorming/bug-patterns.md` | Added PAT-024 |

---

## 4. Open Items After Review

| ID | Type | Description | Target |
|----|------|-------------|--------|
| D-153 | Tech Debt | `tenant.quota_breach` Tier 3 NATS catalog entry | Future quota/notification story |
| D-156 | Tech Debt | `checkQuotaBreachCount` digest NO-OP → live aggregation | Requires D-153 first |
| D-168 | Tech Debt | `compliance.tsx` still uses legacy `useTenantResources` hook | Future FIX |
| D-170 | Tech Debt | 7-day trend endpoint + sparkline in SlidePanel | Future FIX |
| D-171 | Tech Debt | `recent_breaches` field deep payload (event type, actor, meta) | Future FIX |

---

## 5. Final Verdict

**PASS.** All 14 review checks complete. All 8 required doc edits applied. Zero new tech debt introduced in this review pass. FIX-246 is fully closed.
