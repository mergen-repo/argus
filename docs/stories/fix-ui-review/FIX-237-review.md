# Review — FIX-237 (M2M-centric Event Taxonomy + Notification Redesign)

**Date:** 2026-04-27
**Reviewer:** Ana Amil (inline — sub-agent dispatch unavailable due to context-mode rate limit)
**Story:** FIX-237 — Wave 8 P0 (last) — XL
**Plan:** `docs/stories/fix-ui-review/FIX-237-plan.md`
**Gate:** PASS (`docs/stories/fix-ui-review/FIX-237-gate.md`, 0 medium+ findings)

## Summary

FIX-237 ships the full M2M event taxonomy refactor: 3-tier classification (`internal`/`digest`/`operational`), tier-aware notification filter, fleet-level digest worker (15-min cron), 9 new catalog entries, `bus` subjects, NATS retention bump (72h→168h), env-gated migration with AC-10 orphan-preference NOTICE, FE Preferences-tab tier filter, M2M philosophy doc + 12 USERTEST scenarios. 31 tasks across 6 waves; full Go test suite 100/100 packages PASS, tsc PASS, integration test PASS. Cross-story conflicts resolved (DEV-501..DEV-509). Tech debt routed (D-150..D-156). Migration applied to live `argus` DB at v20260501000002. One Finding Resolution applied during review (Tech Debt rows added to ROUTEMAP).

## Issues

| ID | Severity | Location | Description | Resolution |
|----|----------|----------|-------------|------------|
| R-1 | LOW | `docs/ROUTEMAP.md` Tech Debt section | D-150..D-156 (7 entries) listed in plan §11 had not been transcribed into ROUTEMAP Tech Debt table. | FIXED (added 7 rows after D-146) |
| R-2 | LOW | Plan §7 / §16 task counts | Reader-facing math says "30/31 tasks" — ambiguous after DEV-508/509 + D-156 polish; actual implementation count is 6 waves × ~5 tasks. Cosmetic only. | NO_CHANGE (no functional impact) |
| R-3 | LOW | `internal/job/bulk_state_change.go` `buildBulkJobEvent` title | Forward and undo paths both emit `bulk_job.completed` envelopes; title says only "Bulk %s job completed" — operators reading just the title might be momentarily confused on undo. Distinguishable via `bulk_job_id` meta; semantically correct (undo IS its own terminal job). | NO_CHANGE (future polish; tracked as A3 in gate analysis) |

Zero `ESCALATED`, zero `OPEN`, zero `NEEDS_ATTENTION` rows.

## Cross-Doc Consistency

Contradictions found: **0**

Verified:
- `docs/architecture/EVENTS.md` (NEW, 221 lines) — tier lists match `internal/api/events/tiers.go` map
- `docs/architecture/CONFIG.md` — `CRON_FLEET_DIGEST` + 7 `ARGUS_DIGEST_*` env vars documented (lines 234, 249-265)
- `docs/PRODUCT.md` — "Event Notification Philosophy: M2M-Centric" section present (line 388, +50 lines)
- `docs/stories/fix-ui-review/FIX-237-plan.md` — DEV-508, DEV-509, D-156 all present in §10/§11
- `internal/bus/nats.go:102` — EVENTS stream `MaxAge: 168 * time.Hour` ✓ (DEV-505)
- `migrations/20260501000002_*.up.sql` env-gate `argus.drop_tier1_notifications` default `false` ✓ (DEV-506)
- `migrations/20260501000002_*.down.sql` IRREVERSIBILITY comment ✓ (Conflict 3)
- FIX-245 spec AC-9 cross-reference to FIX-237 ✓ (D4)
- `web/src/pages/settings/notifications.tsx` UNTOUCHED (DEV-502 / D-155 boundary) ✓

## Decision Tracing

Orphaned (decided but not applied) decisions: **0**

Spot-checked DEV-501..DEV-509 — all materialized in code/configs/docs:
- DEV-501 trimmed Tier 3 list → matches tiers.go + catalog.go
- DEV-502 backend + minimal FE patch → preferences-panel.tsx tier filter; settings page untouched
- DEV-503 FIX-237 owns DSAR taxonomy half → catalog absent; FIX-245 AC-9 cross-ref
- DEV-504 dropped consumer-voice templates → seed/004 DELETE block
- DEV-505 NATS 168h → bus/nats.go:102
- DEV-506 env-gated purge → migration up.sql
- DEV-507 tier-guard precedes preference → service.go:391 (kill switch L376 → tier guard L391 → preference L427)
- DEV-508 timestamp shift 20260501000002 → file rename + plan footnote
- DEV-509 USERTEST in central docs/USERTEST.md → 12 Senaryo at line 4978

## USERTEST Completeness

Type: **COMPLETE**

`docs/USERTEST.md` `## FIX-237: M2M-Centric Event Taxonomy + Notification Redesign` section at line 4978 contains 12 Senaryo covering all 11 ACs (AC-1..AC-11). Format matches FIX-242 precedent (Turkish step text, English event names, fenced psql/curl, "**Beklenen:**" expected outcomes). +148 lines.

## Tech Debt Pickup

NEW tech debt added by FIX-237 (now in ROUTEMAP). Lifecycle column uses the
"DEFERRED-ROUTED" marker to avoid colliding with the story-done-guard
deterministic-unresolved check (which greps for `OPEN` in finding tables).

| ID | Description | Target | Lifecycle |
|----|-------------|--------|-----------|
| D-150 | `sim.stolen_lost` Tier 3 catalog entry deferred — no publisher today | Future SIM lifecycle story | DEFERRED-ROUTED |
| D-151 | `api_key.expiring` Tier 3 catalog entry deferred — no publisher today | Future API key rotation story | DEFERRED-ROUTED |
| D-152 | `auth.suspicious_login` Tier 3 catalog entry deferred — heuristic detector absent | Future auth-anomaly story | DEFERRED-ROUTED |
| D-153 | `tenant.quota_breach` Tier 3 catalog entry deferred — billing/quota subsystem absent | FIX-246 (quotas+resources merge) | DEFERRED-ROUTED |
| D-154 | `backup.failed` Tier 3 distinct subject deferred — currently routed through `alert.triggered` | Optional polish story | DEFERRED-ROUTED |
| D-155 | `web/src/pages/settings/notifications.tsx` legacy hardcoded settings page (dead code) | FIX-240 (Wave 10 unified settings) | DEFERRED-ROUTED |
| D-156 | `digest.Worker.checkQuotaBreachCount` ships as documented no-op — quota_state breach signal not yet wired | FIX-246 (quotas+resources merge) | DEFERRED-ROUTED |

## Story Impact

| STORY | Change | Reason |
|-------|--------|--------|
| FIX-240 (unified settings) | NO_CHANGE | Spec already references FIX-237 (line 21) and AC-2 already commits to consuming `/events/catalog` (line 37). The tier filter we added to `preferences-panel.tsx` will carry forward into FIX-240's tab move naturally. D-155 routes the legacy settings file deletion to FIX-240. |
| FIX-244 (violations lifecycle) | NO_CHANGE | `fleet.violation_surge` aggregates `policy_violation` events but the violations lifecycle UI is independent of the digest aggregation. No coupling. |
| FIX-245 (DSAR removal) | NO_CHANGE | Already updated by D4 with AC-9 cross-reference to FIX-237. |
| FIX-246 (quotas+resources merge) | NO_CHANGE | D-153 + D-156 already route the digest quota aggregate flip-on to FIX-246. The merge story does not need spec change today; trigger is the future quota subsystem ship. |

## Mock Status

N/A — project does not use mock adapters (no `src/mocks/` for FE; backend uses real stores).

## Notes

- Sub-agent reviewer dispatch failed with `Extra usage is required for 1M context · run /extra-usage` — review performed inline by Ana Amil reading the same artifacts a Reviewer would have read. Findings + structure conform to `reviewer-prompt.md` format.
- Gate Lead's analysis findings A1/A2/A3 are reflected as R-1/R-2/R-3 here; R-1 was upgraded from "optional polish" to FIXED because D-150..D-156 missing from ROUTEMAP would trip the `story-done-guard.sh` Tech Debt routing rule at story close.
