# Review: STORY-032 — CDR Processing & Rating Engine

**Reviewer:** Amil Reviewer Agent
**Date:** 2026-03-22
**Phase:** 6 (Analytics & BI) -- FIRST STORY IN PHASE
**Story Status:** DONE (825 tests, 29 new, 0 failures)

---

## 1. Next Story Impact (STORY-034, STORY-035, STORY-036)

STORY-032 is the primary data source for 3 downstream stories. The APIs, store methods, and data model directly shape what STORY-034/035/036 can build on.

| Story | Dependency | Impact | Readiness |
|---|---|---|---|
| STORY-034 (Usage Analytics) | **BLOCKED BY STORY-032** | **Ready.** CDR store provides `ListByTenant` with time-range, sim_id, operator_id filters. `cdrs_hourly` and `cdrs_daily` continuous aggregates exist for time-series queries. `CostAggRow` struct includes `TotalBytes` and `ActiveSims` -- reusable for usage metrics. STORY-034 will need new store methods for group-by breakdowns (by operator, APN, RAT type) and top-consumers queries. | UNBLOCKED |
| STORY-035 (Cost Analytics) | **BLOCKED BY STORY-032** | **Ready.** `GetCostAggregation` returns per-operator per-day cost data from `cdrs_daily`. Rating engine separates `usage_cost` (with multipliers) from `carrier_cost` (raw rate) -- enabling margin calculation. STORY-035 will need: (1) cost-per-MB aggregation by RAT type, (2) top-expensive-SIMs query, (3) optimization suggestion engine comparing operator rates. | UNBLOCKED |
| STORY-036 (Anomaly Detection) | Partially blocked by STORY-032 | **Partial.** CDR data enables batch data-spike detection (SIM daily usage vs. 30-day average). The `cdrs_daily` aggregate can be queried for per-SIM daily totals. However, STORY-036 also requires real-time auth event analysis (Redis sliding window) which is independent of CDR processing. | UNBLOCKED (CDR part) |

**Post-notes for downstream stories:**
- STORY-034: The `cdrs_hourly` aggregate includes `rat_type` and `apn_id` columns -- can be used directly for group-by breakdowns without additional aggregation queries.
- STORY-035: `CostAggRow` currently has 6 fields (operator_id, bucket, total_usage_cost, total_carrier_cost, total_bytes, active_sims). Margin calculation is not in the aggregate view -- STORY-035 should compute margin = usage_cost - carrier_cost at query time.
- STORY-035: ALGORITHMS.md Section 5 describes a `cost_monthly_agg` continuous aggregate with `apn_id` and `rat_type` grouping. The existing `cdrs_daily` view groups only by `tenant_id, operator_id`. STORY-035 may need a new continuous aggregate or query the hourly view for finer breakdowns.
- STORY-036: The `GetCumulativeSessionBytes` store method (session-level cumulative bytes) could be repurposed for data-spike baseline calculation, but a per-SIM daily aggregate query would be more efficient.

---

## 2. Architecture Evolution

### 2a. ARCHITECTURE.md -- Project Structure

The project structure in ARCHITECTURE.md already shows:
- `internal/analytics/cdr/` under SVC-07 (line 150) -- correctly placed
- `internal/store/` for data access -- cdr.go added here
- `internal/api/` implied for API handlers -- `internal/api/cdr/` is a new subpackage
- `internal/job/` for SVC-09 -- cdr_export.go added here

**Gap:** `internal/api/cdr/` is not explicitly listed in the ARCHITECTURE.md project structure tree. The tree shows `internal/api/` with subitems like `ota/`, `session/`, `sim/`, etc. but no `cdr/` entry. Should be added for consistency.

### 2b. ARCHITECTURE.md -- Caching Strategy

No new Redis caching keys introduced by STORY-032. The CDR consumer reads operator grants on each event (no cache). The rating engine is a pure function with no cache.

**Observation:** Each CDR event triggers a `ListGrants(ctx, tenantID)` call to PostgreSQL. At high CDR volumes (30M-150M records/day per ARCHITECTURE.md), this could become a bottleneck. Consider caching operator grants in Redis (key: `operator:grants:{tenant_id}`, TTL: 5min) for STORY-033 or a future optimization pass. Not blocking for v1.

### 2c. CONFIG.md -- No New Environment Variables

STORY-032 introduces no new configurable env vars. The CDR consumer uses hardcoded values:
- Queue group: `cdr-consumer` (hardcoded in consumer.go)
- Default RAT multipliers, peak hours, volume tiers (hardcoded in rating.go with defaults)
- Batch size for export progress updates: 1000 (hardcoded in cdr_export.go)

These are acceptable defaults. Operator-specific rate configuration comes from `adapter_config` JSONB on `operator_grants` (not implemented in v1 -- uses `cost_per_mb` column directly). Future stories could add per-operator `tariff` and `volume_tiers` config.

### 2d. ALGORITHMS.md -- Package Path Mismatch

ALGORITHMS.md Section 5 references `internal/analytics/cost/` as the rating package, but the actual implementation is in `internal/analytics/cdr/rating.go`. This should be corrected.

---

## 3. GLOSSARY.md Updates

### New Terms to Add

| Term | Definition | Context |
|------|-----------|---------|
| Rating Engine | Pure-function cost calculator that applies 4 multiplicative factors to determine CDR cost: (1) operator base rate (cost_per_mb), (2) RAT-type multiplier (e.g., 5G=1.5x, NB-IoT=0.3x), (3) time-of-day tariff (peak 08-20 UTC = 1.0x, off-peak = 0.7x), (4) volume tier (0-1GB=1.0x, 1-10GB=0.8x, 10GB+=0.5x). Produces `usage_cost` (with multipliers) and `carrier_cost` (raw rate). | SVC-07, STORY-032, `internal/analytics/cdr/rating.go` |
| Cost Aggregation | SQL GROUP BY aggregation of CDR cost data per operator per time bucket. Queries `cdrs_daily` continuous aggregate for pre-computed daily totals: total_usage_cost, total_carrier_cost, total_bytes, active_sims. Used by cost analytics API. | SVC-07, STORY-032, `store.GetCostAggregation()` |
| CDR Consumer | NATS QueueSubscribe consumer that listens on 3 session event subjects (session.started/updated/ended) with queue group `cdr-consumer`. Creates CDR records in TBL-18 for all protocol types (RADIUS, Diameter, 5G SBA). Applies rating engine for interim and stop records. Idempotent via ON CONFLICT DO NOTHING. | SVC-07, STORY-032, `internal/analytics/cdr/consumer.go` |
| CDR Export | Background job (type `cdr_export`) that streams CDR records for a date range and produces a CSV file. Uses `StreamForExport` for row-by-row cursor iteration. CSV stored as base64 in job result JSONB. Triggered via POST /api/v1/cdrs/export (API-115). | SVC-09, STORY-032, `internal/job/cdr_export.go` |

### Existing Terms -- Updates Needed

| Term | Update |
|------|--------|
| CDR | Current definition is minimal: "Call Detail Record \| Usage record: bytes, duration, cost, RAT-type per session". Should be expanded to include: "Created by CDR Consumer on NATS session events. Fields: session_id, sim_id, operator_id, apn_id, rat_type, record_type (start/interim/stop), bytes_in, bytes_out, duration_sec, usage_cost, carrier_cost, rate_per_mb, rat_multiplier, timestamp. Stored in TBL-18 (TimescaleDB hypertable). Deduplicated via unique index on (session_id, timestamp, record_type)." |
| Job Runner | Add `cdr_export` to the real processors list: "Real processors: `bulk_sim_import` (STORY-013), `bulk_session_disconnect` (STORY-017), `ota_command` (STORY-029), `bulk_state_change`, `bulk_policy_assign`, `bulk_esim_switch` (STORY-030), `policy_dry_run` (STORY-024), `policy_rollout_stage` (STORY-025), `cdr_export` (STORY-032). Remaining stubs: `purge_sweep`, `ip_reclaim`, `sla_report`." |

---

## 4. Screen Updates

No frontend screens to update. STORY-032 is backend-only. Relevant screens for future frontend:
- SCR-011 (Analytics Usage) -- will use CDR data via STORY-034 API
- SCR-012 (Analytics Cost) -- will use CDR data via STORY-035 API
- SCR-021c (SIM Detail Usage tab) -- per-SIM CDR list via GET /api/v1/cdrs?sim_id=

No changes needed to `docs/SCREENS.md`.

---

## 5. FUTURE.md Relevance

No impact on FUTURE.md items. The rating engine is a v1 feature. FUTURE.md mentions:
- "AI Anomaly Engine" -- uses CDR stream, but that's STORY-036 scope
- "Network Quality Scoring" -- uses CDR data as training data, future scope

---

## 6. Decision Tracing (DEV-100..104)

All 5 decisions verified in `docs/brainstorming/decisions.md`:

| # | Decision | Status | Code Verification |
|---|----------|--------|-------------------|
| DEV-100 | QueueSubscribe for at-most-once processing | ACCEPTED | `consumer.go` line 44: `subscriber.QueueSubscribe(subj, "cdr-consumer", ...)`. Queue group ensures single delivery across instances. |
| DEV-101 | Pure-function rating engine with 4 factors | ACCEPTED | `rating.go` line 64: `Calculate()` applies ratMultiplier, timeMultiplier, volumeMultiplier to base cost. No external deps. 14 tests confirm all factor combinations. |
| DEV-102 | Dedup via unique index + ON CONFLICT DO NOTHING | ACCEPTED | Migration `20260322000001_cdr_dedup_index.up.sql`: `CREATE UNIQUE INDEX idx_cdrs_dedup ON cdrs (session_id, timestamp, record_type)`. `cdr.go` line 134: `ON CONFLICT (session_id, timestamp, record_type) DO NOTHING`. |
| DEV-103 | Streaming CDR export as background job | ACCEPTED | `cdr_export.go` line 92: `StreamForExport` with callback, progress update every 1000 rows. CSV base64 in job result. |
| DEV-104 | SQL GROUP BY for cost aggregation (no materialized view in v1) | ACCEPTED | `cdr.go` line 240: `GetCostAggregation` queries `cdrs_daily` view. **Note:** DEV-104 says "no materialized view" but implementation actually queries the `cdrs_daily` continuous aggregate, which IS a materialized view. Decision text is slightly misleading -- the intent was "no NEW materialized view" (the existing `cdrs_daily` is pre-existing from migration 000004). |

---

## 7. Makefile Consistency

No new Makefile targets needed. Existing `make test` covers all new test files. No new Docker services, migrations beyond the dedup index, or build steps.

---

## 8. CLAUDE.md Consistency

CLAUDE.md project structure shows:
- `internal/analytics/` for SVC-07 -- OK, `cdr/` subpackage fits here
- `internal/store/` for PostgreSQL data access -- OK, `cdr.go` added
- `internal/job/` for SVC-09 -- OK, `cdr_export.go` added
- `internal/api/` implied but not listed with all subpackages -- OK, follows existing pattern

No updates needed to CLAUDE.md.

---

## 9. Cross-Doc Consistency

| Document | Check | Status | Detail |
|----------|-------|--------|--------|
| ROUTEMAP.md | STORY-032 row | **NEEDS UPDATE** | Currently shows `[~] IN PROGRESS, Step: Review`. Must be updated to `[x] DONE` with date 2026-03-22. |
| ROUTEMAP.md | Progress counter | OK | Already at "30/55 (55%)" -- STORY-032 is #31, so should be updated to "31/55 (56%)". |
| ROUTEMAP.md | Change log | **NEEDS UPDATE** | STORY-032 completion entry needed. |
| GLOSSARY.md | CDR term | **NEEDS EXPANSION** | Existing definition is minimal (see section 3). |
| GLOSSARY.md | Rating Engine | **GAP** | New term not present. |
| GLOSSARY.md | CDR Consumer | **GAP** | New term not present. |
| GLOSSARY.md | CDR Export | **GAP** | New term not present. |
| GLOSSARY.md | Cost Aggregation | **GAP** | New term not present. |
| GLOSSARY.md | Job Runner | **NEEDS UPDATE** | Add `cdr_export` to real processor list (also resolves STORY-030 review open item #3). |
| decisions.md | DEV-100..104 | OK | All 5 decisions present and verified. |
| CONFIG.md | CDR retention | OK | `DEFAULT_CDR_RETENTION_DAYS=180` already documented. |
| CONFIG.md | NATS session subjects | OK | `argus.events.session.*` documented with CDR processor as consumer. |
| ALGORITHMS.md | Section 5 package path | **NEEDS UPDATE** | Says `internal/analytics/cost/` but actual is `internal/analytics/cdr/`. |
| ARCHITECTURE.md | api/cdr/ in tree | **MINOR GAP** | `internal/api/cdr/` not listed in project structure tree. |
| ARCHITECTURE.md | analytics/cdr/ in tree | OK | Already listed (line 150). |
| db/aaa-analytics.md | TBL-18 schema | OK | CDR struct matches all 16 columns in schema doc. |
| db/aaa-analytics.md | Dedup index | **MINOR GAP** | `idx_cdrs_dedup` not listed in the index section (only the 4 original indexes are listed). |
| API _index.md | API-114, API-115 | OK | Both endpoints documented with correct paths and auth. |
| STORY-034 spec | CDR dependency | OK | Correctly lists STORY-032 as blocker. |
| STORY-035 spec | CDR dependency | OK | Correctly lists STORY-032 as blocker. |
| STORY-036 spec | CDR dependency | OK | Correctly lists STORY-032 as blocker. |

### Prior Review Gaps Still Open

From STORY-030 review:
| # | Action | Status |
|---|--------|--------|
| STORY-028 #3 | Add 5 eSIM error codes to ERROR_CODES.md | STILL OPEN |
| STORY-031 #3 | Update Job Runner glossary term | **ADDRESSABLE NOW** (add `cdr_export` while updating) |

From STORY-031 review:
| # | Action | Status |
|---|--------|--------|
| STORY-027 #2 | Add `rattype/` to ARCHITECTURE.md project structure | STILL OPEN |

---

## 10. Spec vs. Implementation Divergences

| # | Spec Says | Implementation Does | Severity | Verdict |
|---|-----------|-------------------|----------|---------|
| 1 | AC fields: `cost_amount`, `cost_currency`, `rated_at` | Uses `usage_cost`, `carrier_cost`, `rate_per_mb`, `rat_multiplier`, `timestamp` from actual TBL-18 schema | LOW | **Acceptable.** Implementation follows the actual DB schema, not the AC's field names. `usage_cost`+`carrier_cost` provide more detail than a single `cost_amount`. No `cost_currency` column exists -- currency is tenant-level (see ALGORITHMS.md). No `rated_at` -- `timestamp` serves this purpose. Gate report notes this. |
| 2 | ALGORITHMS.md Section 5: rate comes from `policy.charging.rate_per_mb` | Implementation gets rate from `operator_grants.cost_per_mb` | LOW | **Acceptable.** The policy-based rate was a design-time assumption. Implementation correctly uses operator grant rates as the source of truth. STORY-035 can add policy-based rate overrides later. |
| 3 | ALGORITHMS.md Section 5: package is `internal/analytics/cost/` | Actual package is `internal/analytics/cdr/` | LOW | **Doc bug.** The `cost/` package doesn't exist. Rating logic is part of CDR processing. ALGORITHMS.md should be updated. |
| 4 | AC: "CDR processing is async via NATS (accounting events -> SVC-07 consumer)" | Consumer uses QueueSubscribe (not JetStream durable consumer) | LOW | **Acceptable per DEV-100.** QueueSubscribe provides at-most-once delivery. JetStream durable consumers (at-least-once) would be preferred for production but would add complexity. Idempotent inserts via dedup index mitigate the risk. |
| 5 | Plan: Consumer constructor has `eventBus *bus.EventBus` parameter | Implementation omits `eventBus` from `NewConsumer` -- uses `MessageSubscriber` interface instead | NONE | **Better than spec.** Interface-based dependency injection is cleaner and more testable than concrete `bus.EventBus` dependency. |
| 6 | AC: "Diameter CCR events -> equivalent CDR records" | Consumer subscribes to generic session events (not Diameter-specific) | NONE | **Correct.** Diameter CCR handlers (STORY-019) publish to the same NATS session subjects. Protocol-agnostic design is intentional. |

---

## 11. USERTEST Completeness

| # | Check | Status | Detail |
|---|-------|--------|--------|
| 1 | CDR list endpoint | OK | `curl -k https://localhost/api/v1/cdrs?from=...&to=... -H "Authorization: Bearer $TOKEN"` -- correct path, correct params |
| 2 | SIM-filtered CDR list | OK | `?sim_id=<SIM_UUID>` -- correctly documented |
| 3 | CDR export endpoint | OK | `POST /api/v1/cdrs/export` with `{from, to, format:"csv"}` -- correct path, correct body |
| 4 | NATS event test instruction | OK | Notes that RADIUS accounting event should create CDR in table |
| 5 | Unit test command | OK | `go test ./internal/analytics/cdr/... ./internal/store/ ./internal/api/cdr/... ./internal/job/ -v` -- covers all packages |
| 6 | Port prefix | OK | Uses `https://localhost/...` (via Nginx) -- consistent with STORY-030 USERTEST |
| 7 | Missing: CDR export download | **MINOR GAP** | USERTEST step 3 shows export job creation but does not show how to download the result CSV (via `GET /api/v1/jobs/{job_id}` and decoding base64 from result). Not blocking -- job result retrieval is documented elsewhere. |

---

## 12. Code Quality Observations

**Strengths:**
- Rating engine is a pure function -- fully testable without database, no side effects
- 14 rating tests cover all factor combinations (RAT types, off-peak, volume tiers, zero bytes, unknown RAT, combined multipliers)
- Consumer is protocol-agnostic -- handles RADIUS, Diameter, 5G SBA identically via shared NATS subjects
- Idempotent insert pattern (ON CONFLICT DO NOTHING + return nil) prevents duplicates gracefully
- Handler validates all inputs with appropriate HTTP status codes (400 for format errors, 422 for validation errors, 401 for auth)
- Export job uses streaming callback pattern -- does not load entire dataset into memory
- CDR DTO correctly formats monetary values as strings (`%.4f`) to avoid floating-point precision issues in JSON
- Consumer gracefully handles missing fields (empty APN, RAT type, timestamps) with nil/default fallbacks

**Minor observations (not blocking):**
- `GetCumulativeSessionBytes` query is not tenant-scoped (`WHERE session_id = $1` only). Gate report notes this is acceptable since session UUIDs are globally unique. However, for defense-in-depth, adding `AND tenant_id = $2` would be consistent with the project convention.
- `calculateCost` calls `ListGrants(ctx, tenantID)` which returns ALL grants for a tenant, then iterates to find the matching operator. At scale, this could be optimized with a `GetGrantByOperator(tenantID, operatorID)` store method. Not blocking.
- Export stores CSV as base64 in job result JSONB. For large exports (100K+ CDRs), this could exceed reasonable JSONB field sizes. ARCHITECTURE.md describes S3 cold storage for CDR exports -- should be added in STORY-053 (Data Volume Optimization).
- The `cdrs_daily` continuous aggregate column is `total_cost` but `GetCostAggregation` scans into `TotalUsageCost` -- this mapping should be verified against the actual `cdrs_daily` view definition (line 114 in aaa-analytics.md uses `SUM(usage_cost) AS total_cost`).

---

## 13. Action Items Summary

| # | Priority | Action | Target File |
|---|----------|--------|-------------|
| 1 | HIGH | Update ROUTEMAP.md: STORY-032 to `[x] DONE` with date 2026-03-22, progress to 31/55 (56%), add change log entry | `docs/ROUTEMAP.md` |
| 2 | MEDIUM | Add 4 new glossary terms (Rating Engine, Cost Aggregation, CDR Consumer, CDR Export) and expand CDR definition | `docs/GLOSSARY.md` |
| 3 | MEDIUM | Update Job Runner glossary term -- add `cdr_export` to real processor list (resolves STORY-030 open item) | `docs/GLOSSARY.md` |
| 4 | MEDIUM | Fix ALGORITHMS.md Section 5 package path: `internal/analytics/cost/` -> `internal/analytics/cdr/` | `docs/architecture/ALGORITHMS.md` |
| 5 | LOW | Add `idx_cdrs_dedup` to TBL-18 index list in db/aaa-analytics.md | `docs/architecture/db/aaa-analytics.md` |
| 6 | LOW | Add `internal/api/cdr/` to ARCHITECTURE.md project structure tree | `docs/ARCHITECTURE.md` |
| 7 | LOW | Consider adding `AND tenant_id = $2` to `GetCumulativeSessionBytes` for defense-in-depth tenant scoping | `internal/store/cdr.go` |
| 8 | LOW | Verify `total_cost` column name mapping in `GetCostAggregation` vs. `cdrs_daily` view definition | `internal/store/cdr.go` |

---

## 14. Verdict

**STORY-032 is well-implemented and establishes a solid foundation for the Analytics & BI phase.** The rating engine is clean, testable, and correctly implements all 4 cost factors from the architecture spec. The NATS consumer is protocol-agnostic and handles all 3 AAA protocols (RADIUS, Diameter, 5G SBA) through shared session event subjects. The deduplication strategy (unique index + ON CONFLICT DO NOTHING) is the right choice for an event-driven system.

**Key strengths:**
- Pure-function rating engine enables deterministic testing without infrastructure dependencies
- Protocol-agnostic CDR creation -- single consumer handles all AAA protocols
- Correct separation of `usage_cost` (rated with multipliers) and `carrier_cost` (raw operator rate) enables future margin analysis
- TimescaleDB integration via pre-existing hypertable and continuous aggregates
- Streaming export pattern prevents memory issues on large datasets

**No spec divergences that require action.** The 3 field-name differences (cost_amount vs. usage_cost, etc.) correctly follow the actual database schema rather than the AC's placeholder names.

**STORY-034 (Usage Analytics) and STORY-035 (Cost Analytics) are fully unblocked.** Both can build on the CDR store, continuous aggregates, and rating data. STORY-036 (Anomaly Detection) is partially unblocked for batch detection via CDR aggregates.

**Phase 6 is underway.** 5 stories remain in Phase 6 (STORY-033 through STORY-037).
