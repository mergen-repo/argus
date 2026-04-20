# FIX-210: Alert Deduplication + State Machine (Edge-triggered, Cooldown)

## Problem Statement
Without dedup, same root cause fires alerts continuously (e.g., operator healthcheck fails every 30s for 2 hours → 240 identical alerts). Floods UI + notification channels + storage.

## User Story
As an SRE, I want alerts to be deduplicated on identity (tenant/type/source/entity) — first occurrence opens a new alert, subsequent same-signature events increment `occurrence_count` + update `last_seen_at` until resolved. Prevents alarm fatigue.

## Architecture Reference
- Extends FIX-209 `alerts` table
- Edge-triggered detection: status flips detected via "previous_state != current_state" check
- Cooldown — prevents flapping (same alert can't re-fire within cooldown window)

## Findings Addressed
F-08

## Acceptance Criteria
- [ ] **AC-1:** `alerts` table augmented: `dedupe_key VARCHAR(255) NOT NULL`, `occurrence_count INT DEFAULT 1`, `first_seen_at`, `last_seen_at`, `cooldown_until TIMESTAMPTZ NULL`.
- [ ] **AC-2:** `dedupe_key` computed = SHA256(`tenant_id | type | source | sim_id|operator_id|apn_id | severity`).
- [ ] **AC-3:** `INSERT ... ON CONFLICT (dedupe_key) WHERE state='open' DO UPDATE SET occurrence_count += 1, last_seen_at = NOW()` — atomic dedupe via unique partial index.
- [ ] **AC-4:** Edge detection — publishers only fire on status change (e.g., operator health_status: healthy→degraded transition), NOT on every health check run. Healthcheck worker tracks previous state.
- [ ] **AC-5:** Cooldown — after alert resolved, same dedupe_key can't re-fire for N minutes (default 5min). Env: `ALERT_COOLDOWN_MINUTES=5`.
- [ ] **AC-6:** UI alert list shows occurrence_count — "5× in last 2h" instead of 5 separate rows.
- [ ] **AC-7:** Metrics — Prometheus counter `argus_alerts_deduplicated_total{type}` tracks dedup effectiveness.

## Files to Touch
- `migrations/YYYYMMDDHHMMSS_alerts_dedupe.up.sql`
- `internal/notification/service.go::handleAlert` — dedupe key compute + upsert
- Publishers (health_worker, enforcer, etc.) — edge-trigger guard
- `web/src/pages/alerts/index.tsx` — display occurrence_count

## Risks & Regression
- **Risk 1 — Missed distinct events:** Too-aggressive dedup merges unrelated events. Mitigation: AC-2 includes entity_id, so "Operator A degraded" ≠ "Operator B degraded".
- **Risk 2 — Edge-trigger miss on startup:** Worker restart loses previous_state. Mitigation: persist last_state per entity in Redis.

## Test Plan
- Unit: dedupe key uniqueness; 100 fires → 1 row with count=100
- Integration: flapping scenario (healthy/degraded alternation) — no alerts during cooldown

## Plan Reference
Priority: P0 · Effort: M · Wave: 2
